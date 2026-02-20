package pockettts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ServerOptions controls how the pocket-tts HTTP server is started and used.
type ServerOptions struct {
	// Host that the server listens on (default: "localhost").
	Host string

	// Port that the server listens on (default: 8000).
	Port int

	// Voice is the default voice name or path passed to `pocket-tts serve`.
	Voice string

	// Config is an optional path to a pocket-tts config file.
	Config string

	// ExecutablePath overrides the default "pocket-tts" binary name/path.
	ExecutablePath string

	// LogWriter receives stderr output from the server process.
	// If nil, stderr is discarded.
	LogWriter io.Writer

	// StartupTimeout is how long to wait for the server to become healthy
	// after starting. Defaults to 5 minutes (model download may be needed).
	StartupTimeout time.Duration
}

func (o *ServerOptions) host() string {
	if o.Host == "" {
		return "localhost"
	}
	return o.Host
}

func (o *ServerOptions) port() int {
	if o.Port == 0 {
		return 8000
	}
	return o.Port
}

func (o *ServerOptions) baseURL() string {
	return fmt.Sprintf("http://%s:%d", o.host(), o.port())
}

func (o *ServerOptions) startupTimeout() time.Duration {
	if o.StartupTimeout <= 0 {
		return 5 * time.Minute
	}
	return o.StartupTimeout
}

// ServerClient manages a pocket-tts HTTP server and issues TTS requests to it.
//
// Unlike the CLI-based Client, ServerClient keeps the model warm in memory
// between calls, which dramatically reduces per-request latency after the
// first request.
//
// Create with NewServerClient. Call Start to launch the server process, then
// Generate for each TTS request, and Stop when done.
type ServerClient struct {
	opts   ServerOptions
	proc   *exec.Cmd
	http   *http.Client
}

// NewServerClient creates a ServerClient with the given options.
// Call Start to actually launch the pocket-tts server process.
func NewServerClient(opts ServerOptions) *ServerClient {
	return &ServerClient{
		opts: opts,
		http: &http.Client{Timeout: 10 * time.Minute},
	}
}

// Start launches `pocket-tts serve` as a managed subprocess and waits until
// the /health endpoint responds. Returns an error if the server does not become
// healthy within ServerOptions.StartupTimeout.
func (s *ServerClient) Start(ctx context.Context) error {
	exe := s.opts.ExecutablePath
	if exe == "" {
		exe = "pocket-tts"
	}

	args := []string{
		"serve",
		"--host", s.opts.host(),
		"--port", fmt.Sprintf("%d", s.opts.port()),
		"--no-reload",
	}
	if s.opts.Voice != "" {
		args = append(args, "--voice", s.opts.Voice)
	}
	if s.opts.Config != "" {
		args = append(args, "--config", s.opts.Config)
	}

	cmd := exec.CommandContext(ctx, exe, args...)
	if s.opts.LogWriter != nil {
		cmd.Stderr = s.opts.LogWriter
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return &ErrExecutableNotFound{Executable: exe}
		}
		return fmt.Errorf("pockettts: start server: %w", err)
	}
	s.proc = cmd

	// Poll /health until healthy or timeout.
	deadline := time.Now().Add(s.opts.startupTimeout())
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_ = s.proc.Process.Kill()
			return ctx.Err()
		}
		if err := s.Health(ctx); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	_ = s.proc.Process.Kill()
	return fmt.Errorf("pockettts: server did not become healthy within %s", s.opts.startupTimeout())
}

// Stop terminates the managed server process. It is safe to call even if Start
// was never called or the process has already exited.
func (s *ServerClient) Stop() error {
	if s.proc == nil || s.proc.Process == nil {
		return nil
	}
	if err := s.proc.Process.Kill(); err != nil {
		return fmt.Errorf("pockettts: stop server: %w", err)
	}
	_ = s.proc.Wait()
	return nil
}

// Health calls GET /health and returns nil if the server responds with status
// 200. This can be used independently of Start for externally-managed servers.
func (s *ServerClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.opts.baseURL()+"/health", nil)
	if err != nil {
		return fmt.Errorf("pockettts: build health request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("pockettts: health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pockettts: health check returned status %d", resp.StatusCode)
	}
	return nil
}

// ServerGenerateOptions controls per-request parameters for server-mode TTS.
type ServerGenerateOptions struct {
	// VoiceURL is a URL (http://, https://, or hf://) to a voice audio file.
	// Mutually exclusive with VoiceWAVPath.
	VoiceURL string

	// VoiceWAVPath is a local path to a voice WAV or .safetensors file to
	// upload for voice cloning. Mutually exclusive with VoiceURL.
	VoiceWAVPath string
}

// Generate sends a POST /tts request to the running pocket-tts server and
// returns the resulting WAV audio.
//
// opts may be nil (uses the server's default voice).
func (s *ServerClient) Generate(ctx context.Context, text string, opts *ServerGenerateOptions) (*WAVResult, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyText
	}
	if opts == nil {
		opts = &ServerGenerateOptions{}
	}

	body, contentType, err := buildTTSRequest(text, opts)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.opts.baseURL()+"/tts", body)
	if err != nil {
		return nil, fmt.Errorf("pockettts: build TTS request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	start := time.Now()
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pockettts: TTS request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &ErrNonZeroExit{ExitCode: resp.StatusCode, Stderr: string(errBody)}
	}

	wavBytes, err := io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("pockettts: read TTS response: %w", err)
	}

	sr, ch, bps, err := parseWAVHeader(wavBytes)
	if err != nil {
		return nil, err
	}

	if s.opts.LogWriter != nil {
		fmt.Fprintf(s.opts.LogWriter, "pockettts: generated %d bytes in %s (mode=server)\n",
			len(wavBytes), elapsed.Round(time.Millisecond))
	}

	return &WAVResult{
		Data:          wavBytes,
		SampleRate:    sr,
		Channels:      ch,
		BitsPerSample: bps,
		Stats:         GenerationStats{Duration: elapsed},
	}, nil
}

// buildTTSRequest constructs the multipart/form-data body for POST /tts.
func buildTTSRequest(text string, opts *ServerGenerateOptions) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("text", text); err != nil {
		return nil, "", fmt.Errorf("pockettts: write text field: %w", err)
	}

	if opts.VoiceURL != "" {
		if err := w.WriteField("voice_url", opts.VoiceURL); err != nil {
			return nil, "", fmt.Errorf("pockettts: write voice_url field: %w", err)
		}
	} else if opts.VoiceWAVPath != "" {
		f, err := os.Open(opts.VoiceWAVPath)
		if err != nil {
			return nil, "", fmt.Errorf("pockettts: open voice file: %w", err)
		}
		defer f.Close()

		part, err := w.CreateFormFile("voice_wav", filepath.Base(opts.VoiceWAVPath))
		if err != nil {
			return nil, "", fmt.Errorf("pockettts: create form file: %w", err)
		}
		if _, err := io.Copy(part, f); err != nil {
			return nil, "", fmt.Errorf("pockettts: copy voice file: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("pockettts: close multipart writer: %w", err)
	}

	return &buf, w.FormDataContentType(), nil
}

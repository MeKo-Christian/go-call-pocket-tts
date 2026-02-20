package pockettts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeServer spins up a local httptest.Server that mimics pocket-tts serve.
type fakeServer struct {
	ts         *httptest.Server
	healthCode int
	ttsCode    int
	ttsBody    []byte
}

func newFakeServer(healthCode, ttsCode int, ttsBody []byte) *fakeServer {
	fs := &fakeServer{healthCode: healthCode, ttsCode: ttsCode, ttsBody: ttsBody}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(fs.healthCode)
	})
	mux.HandleFunc("/tts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.WriteHeader(fs.ttsCode)
		_, _ = w.Write(fs.ttsBody)
	})
	fs.ts = httptest.NewServer(mux)
	return fs
}

func serverClientFor(ts *httptest.Server) *ServerClient {
	// Parse host and port from the test server URL.
	addr := ts.Listener.Addr().String() // e.g. "127.0.0.1:NNNNN"
	parts := strings.SplitN(addr, ":", 2)
	port, _ := strconv.Atoi(parts[1])
	sc := NewServerClient(ServerOptions{Host: parts[0], Port: port})
	return sc
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestServerClient_Health_OK(t *testing.T) {
	fs := newFakeServer(http.StatusOK, http.StatusOK, nil)
	defer fs.ts.Close()

	sc := serverClientFor(fs.ts)
	if err := sc.Health(context.Background()); err != nil {
		t.Fatalf("unexpected health error: %v", err)
	}
}

func TestServerClient_Health_NotOK(t *testing.T) {
	fs := newFakeServer(http.StatusServiceUnavailable, http.StatusOK, nil)
	defer fs.ts.Close()

	sc := serverClientFor(fs.ts)
	if err := sc.Health(context.Background()); err == nil {
		t.Fatal("expected health error for 503")
	}
}

// ---------------------------------------------------------------------------
// Generate
// ---------------------------------------------------------------------------

func TestServerClient_Generate_EmptyText(t *testing.T) {
	sc := NewServerClient(ServerOptions{})
	_, err := sc.Generate(context.Background(), "  ", nil)
	if !errors.Is(err, ErrEmptyText) {
		t.Errorf("expected ErrEmptyText, got %v", err)
	}
}

func TestServerClient_Generate_ValidWAV(t *testing.T) {
	wav := makeWAVHeader(24000, 1, 16)
	wav = append(wav, make([]byte, 100)...) // pad some audio data

	fs := newFakeServer(http.StatusOK, http.StatusOK, wav)
	defer fs.ts.Close()

	sc := serverClientFor(fs.ts)
	result, err := sc.Generate(context.Background(), "Hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SampleRate != 24000 {
		t.Errorf("SampleRate: got %d, want 24000", result.SampleRate)
	}
	if result.Channels != 1 {
		t.Errorf("Channels: got %d, want 1", result.Channels)
	}
}

func TestServerClient_Generate_ServerError(t *testing.T) {
	fs := newFakeServer(http.StatusOK, http.StatusInternalServerError, []byte("model error"))
	defer fs.ts.Close()

	sc := serverClientFor(fs.ts)
	_, err := sc.Generate(context.Background(), "Hello", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	var exitErr *ErrNonZeroExit
	if !errors.As(err, &exitErr) {
		t.Errorf("expected ErrNonZeroExit, got %T: %v", err, err)
	}
	if exitErr.ExitCode != 500 {
		t.Errorf("expected exit code 500, got %d", exitErr.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// buildTTSRequest
// ---------------------------------------------------------------------------

func TestBuildTTSRequest_TextOnly(t *testing.T) {
	body, ct, err := buildTTSRequest("hello world", &ServerGenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("expected multipart content-type, got %s", ct)
	}
	_ = body
}

func TestBuildTTSRequest_WithVoiceURL(t *testing.T) {
	body, _, err := buildTTSRequest("hi", &ServerGenerateOptions{VoiceURL: "https://example.com/voice.wav"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = body
}

// ---------------------------------------------------------------------------
// Golden server test: runs only when pocket-tts is on PATH
// ---------------------------------------------------------------------------

func TestServerClient_Golden(t *testing.T) {
	if _, err := exec.LookPath("pocket-tts"); err != nil {
		t.Skip("pocket-tts not found on PATH; skipping server golden test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sc := NewServerClient(ServerOptions{
		Port:           18765, // use non-default port to avoid collisions
		StartupTimeout: 5 * time.Minute,
	})

	if err := sc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sc.Stop()

	result, err := sc.Generate(ctx, "Server mode test.", nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(result.Data[0:4]) != "RIFF" {
		t.Error("output does not start with RIFF header")
	}
	t.Logf("server WAV: %d bytes, %d Hz, %d ch, %d-bit",
		len(result.Data), result.SampleRate, result.Channels, result.BitsPerSample)
}

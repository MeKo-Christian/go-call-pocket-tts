// Package pockettts provides a Go wrapper around the pocket-tts CLI tool.
//
// It spawns the `pocket-tts generate` subprocess, pipes text in via stdin,
// and returns the resulting WAV audio as bytes.
//
// Basic usage:
//
//	wav, err := pockettts.Generate(ctx, "Hello world", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	os.WriteFile("out.wav", wav, 0644)
package pockettts

import (
	"context"
	"io"
	"time"
)

// Options controls optional parameters passed to the pocket-tts CLI.
// All zero values use the CLI's built-in defaults.
type Options struct {
	// Voice is a built-in voice name (e.g. "mimi") or a path to a
	// .safetensors voice-embedding file produced by pocket-tts export-voice.
	Voice string

	// Config is an optional path to a pocket-tts config JSON/TOML file.
	Config string

	// Temperature controls generation randomness (CLI: --temperature).
	// Zero means use the CLI default.
	Temperature float64

	// LSDDecodeSteps overrides --lsd-decode-steps.
	// Zero means use the CLI default.
	LSDDecodeSteps int

	// NoiseClamp overrides --noise-clamp.
	// Zero means use the CLI default.
	NoiseClamp float64

	// EOSThreshold overrides --eos-threshold.
	// Zero means use the CLI default.
	EOSThreshold float64

	// FramesAfterEOS overrides --frames-after-eos.
	// Zero means use the CLI default.
	FramesAfterEOS int

	// MaxTokens caps the number of generated tokens (CLI: --max-tokens).
	// Zero means use the CLI default.
	MaxTokens int

	// Quiet suppresses informational output from the CLI (CLI: --quiet).
	Quiet bool

	// ExecutablePath overrides the default "pocket-tts" binary name/path.
	// Useful when the executable is not on PATH.
	ExecutablePath string

	// LogWriter receives stderr output from the CLI subprocess.
	// If nil, stderr is discarded.
	LogWriter io.Writer

	// Concurrency is the maximum number of concurrent pocket-tts subprocesses
	// allowed by a Client. Zero or negative means unlimited.
	// Each subprocess loads the model into memory, so keep this low on
	// memory-constrained machines.
	Concurrency int
}

// GenerationStats holds observability data for a single TTS call.
type GenerationStats struct {
	// Duration is the wall-clock time from sending the request until the
	// full WAV was received.
	Duration time.Duration
}

// WAVResult holds the generated audio together with basic metadata.
type WAVResult struct {
	// Data contains the raw WAV file bytes (including RIFF header).
	Data []byte

	// SampleRate is parsed from the WAV header. Pocket-tts produces 24000 Hz.
	SampleRate uint32

	// Channels is parsed from the WAV header.
	Channels uint16

	// BitsPerSample is parsed from the WAV header.
	BitsPerSample uint16

	// Stats contains timing and observability data for this call.
	Stats GenerationStats
}

// Generate calls `pocket-tts generate --text - --output-path -` with the
// provided text and returns the resulting WAV audio.
//
// The ctx can be used to enforce a deadline or cancel the subprocess.
// opts may be nil (uses all defaults).
//
// Returns ErrEmptyText if text is empty or whitespace-only.
// Returns ErrExecutableNotFound if the pocket-tts binary cannot be located.
// Returns ErrProcessTimeout if the context deadline is exceeded.
func Generate(ctx context.Context, text string, opts *Options) (*WAVResult, error) {
	if opts == nil {
		opts = &Options{}
	}
	c := newClient(opts)
	return c.generate(ctx, text)
}

// Client wraps shared configuration so you can reuse it across calls.
// Use NewClient instead of constructing directly.
type Client struct {
	opts Options
	sem  chan struct{} // nil means unlimited
}

// NewClient creates a reusable client with the given options.
// It applies a worker-pool limit via the Concurrency field if > 0.
func NewClient(opts Options) *Client {
	return newClient(&opts)
}

// Generate is the same as the package-level Generate but uses the Client's
// shared options (merged with per-call opts when you extend this later).
func (c *Client) Generate(ctx context.Context, text string) (*WAVResult, error) {
	return c.generate(ctx, text)
}

// Preflight checks that the pocket-tts executable is resolvable.
// It does NOT run a full generation; it only verifies the binary exists.
// Returns nil on success, or an error describing what is missing.
func Preflight(executablePath string) error {
	return preflight(executablePath)
}

// ExportVoiceOptions controls optional parameters for the export-voice command.
type ExportVoiceOptions struct {
	// Config is an optional path to a pocket-tts config file or model variant.
	Config string

	// Quiet suppresses informational output from the CLI (CLI: --quiet).
	Quiet bool

	// ExecutablePath overrides the default "pocket-tts" binary name/path.
	ExecutablePath string

	// LogWriter receives stderr output from the CLI subprocess.
	// If nil, stderr is discarded.
	LogWriter io.Writer
}

// ExportVoice runs `pocket-tts export-voice <audioPath> <exportPath>` to
// convert an audio file into a .safetensors voice-embedding file that can be
// loaded quickly via Options.Voice in subsequent Generate calls.
//
// This is a one-time offline step; the produced .safetensors file can be
// reused across restarts without re-encoding the audio prompt each time.
func ExportVoice(ctx context.Context, audioPath, exportPath string, opts *ExportVoiceOptions) error {
	if opts == nil {
		opts = &ExportVoiceOptions{}
	}
	return exportVoice(ctx, audioPath, exportPath, opts)
}

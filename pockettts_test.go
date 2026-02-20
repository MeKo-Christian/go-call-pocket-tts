package pockettts

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// WAV header parsing
// ---------------------------------------------------------------------------

func TestParseWAVHeader_Valid(t *testing.T) {
	// Minimal 44-byte PCM WAV header: 24000 Hz, mono, 16-bit
	hdr := makeWAVHeader(24000, 1, 16)
	sr, ch, bps, err := parseWAVHeader(hdr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr != 24000 {
		t.Errorf("SampleRate: got %d, want 24000", sr)
	}
	if ch != 1 {
		t.Errorf("Channels: got %d, want 1", ch)
	}
	if bps != 16 {
		t.Errorf("BitsPerSample: got %d, want 16", bps)
	}
}

func TestParseWAVHeader_TooShort(t *testing.T) {
	_, _, _, err := parseWAVHeader([]byte("RIFF"))
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestParseWAVHeader_NotWAV(t *testing.T) {
	data := make([]byte, 44)
	copy(data[0:], "MP3 ")
	_, _, _, err := parseWAVHeader(data)
	if err == nil {
		t.Fatal("expected error for non-WAV data")
	}
}

// ---------------------------------------------------------------------------
// Argument building
// ---------------------------------------------------------------------------

func TestBuildArgs_Defaults(t *testing.T) {
	c := newClient(&Options{})
	args := c.buildArgs()
	mustContain(t, args, "generate")
	mustContain(t, args, "--text")
	mustContain(t, args, "-")
	mustContain(t, args, "--output-path")
}

func TestBuildArgs_AllFlags(t *testing.T) {
	c := newClient(&Options{
		Voice:          "mimi",
		Config:         "/tmp/cfg.json",
		Temperature:    0.8,
		LSDDecodeSteps: 32,
		NoiseClamp:     1.5,
		EOSThreshold:   0.5,
		FramesAfterEOS: 4,
		MaxTokens:      512,
		Quiet:          true,
	})
	args := c.buildArgs()
	pairMustExist(t, args, "--voice", "mimi")
	pairMustExist(t, args, "--config", "/tmp/cfg.json")
	pairMustExist(t, args, "--temperature", "0.8")
	pairMustExist(t, args, "--lsd-decode-steps", "32")
	pairMustExist(t, args, "--noise-clamp", "1.5")
	pairMustExist(t, args, "--eos-threshold", "0.5")
	pairMustExist(t, args, "--frames-after-eos", "4")
	pairMustExist(t, args, "--max-tokens", "512")
	mustContain(t, args, "--quiet")
}

func TestBuildArgs_ZeroValuesOmitted(t *testing.T) {
	c := newClient(&Options{})
	args := c.buildArgs()
	for _, flag := range []string{
		"--voice", "--config", "--temperature", "--lsd-decode-steps",
		"--noise-clamp", "--eos-threshold", "--frames-after-eos",
		"--max-tokens", "--quiet",
	} {
		for _, a := range args {
			if a == flag {
				t.Errorf("flag %q should be absent when option is zero, but found in args %v", flag, args)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Input validation
// ---------------------------------------------------------------------------

func TestGenerate_EmptyText(t *testing.T) {
	_, err := Generate(context.Background(), "", nil)
	if !errors.Is(err, ErrEmptyText) {
		t.Errorf("expected ErrEmptyText, got %v", err)
	}
}

func TestGenerate_WhitespaceText(t *testing.T) {
	_, err := Generate(context.Background(), "   \t\n  ", nil)
	if !errors.Is(err, ErrEmptyText) {
		t.Errorf("expected ErrEmptyText, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Preflight
// ---------------------------------------------------------------------------

func TestPreflight_NotFound(t *testing.T) {
	err := Preflight("/nonexistent/pocket-tts")
	if err == nil {
		t.Fatal("expected error for non-existent executable")
	}
	var notFound *ErrExecutableNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrExecutableNotFound, got %T: %v", err, err)
	}
}

func TestPreflight_Found(t *testing.T) {
	// Use "go" as a known-present executable.
	if err := Preflight("go"); err != nil {
		t.Errorf("unexpected error for known executable: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrency limiter
// ---------------------------------------------------------------------------

func TestConcurrencyLimiter_ContextCancelled(t *testing.T) {
	// Fill the semaphore completely, then check that a new call respects ctx cancellation.
	c := newClient(&Options{Concurrency: 1})
	// Hold the slot manually.
	c.sem <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Generate(ctx, "hello")
	if err == nil {
		t.Fatal("expected error when concurrency slot is full and ctx times out")
	}
	// Must be a timeout error
	var tErr *ErrProcessTimeout
	if !errors.As(err, &tErr) {
		t.Errorf("expected ErrProcessTimeout, got %T: %v", err, tErr)
	}
}

// ---------------------------------------------------------------------------
// Runner: executable not found
// ---------------------------------------------------------------------------

func TestRunner_ExecutableNotFound(t *testing.T) {
	r := &runner{executablePath: "/nonexistent/pocket-tts"}
	_, err := r.run(context.Background(), []string{"generate"}, []byte("hi"))
	if err == nil {
		t.Fatal("expected error")
	}
	var notFound *ErrExecutableNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrExecutableNotFound, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// Runner: non-zero exit
// ---------------------------------------------------------------------------

func TestRunner_NonZeroExit(t *testing.T) {
	// Use "false" (exits 1) as a stand-in for a failing process.
	falsePath, err := exec.LookPath("false")
	if err != nil {
		t.Skip("'false' not found on PATH")
	}
	r := &runner{executablePath: falsePath}
	_, runErr := r.run(context.Background(), nil, nil)
	if runErr == nil {
		t.Fatal("expected error from non-zero exit")
	}
	var exitErr *ErrNonZeroExit
	if !errors.As(runErr, &exitErr) {
		t.Errorf("expected ErrNonZeroExit, got %T: %v", runErr, runErr)
	}
	if exitErr.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// Runner: context timeout
// ---------------------------------------------------------------------------

func TestRunner_Timeout(t *testing.T) {
	// "sleep 10" will be killed by a short context deadline.
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("'sleep' not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := &runner{executablePath: sleepPath}
	_, runErr := r.run(ctx, []string{"10"}, nil)
	if runErr == nil {
		t.Fatal("expected timeout error")
	}
	var tErr *ErrProcessTimeout
	if !errors.As(runErr, &tErr) {
		t.Errorf("expected ErrProcessTimeout, got %T: %v", runErr, runErr)
	}
}

// ---------------------------------------------------------------------------
// Runner: stderr forwarded to LogWriter
// ---------------------------------------------------------------------------

func TestRunner_StderrForwarded(t *testing.T) {
	// Use a shell command that writes to stderr and exits 1.
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("'sh' not found on PATH")
	}
	var logBuf bytes.Buffer
	r := &runner{
		executablePath: shPath,
		logWriter:      &logBuf,
	}
	_, _ = r.run(context.Background(), []string{"-c", "echo myerror >&2; exit 1"}, nil)
	if !strings.Contains(logBuf.String(), "myerror") {
		t.Errorf("stderr not forwarded to logWriter; got: %q", logBuf.String())
	}
}

// ---------------------------------------------------------------------------
// Golden test: runs only when pocket-tts is available
// ---------------------------------------------------------------------------

func TestGenerate_Golden(t *testing.T) {
	if _, err := exec.LookPath("pocket-tts"); err != nil {
		t.Skip("pocket-tts not found on PATH; skipping golden test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := Generate(ctx, "Hello.", &Options{Quiet: true})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(result.Data) < 44 {
		t.Fatalf("WAV too short: %d bytes", len(result.Data))
	}
	if string(result.Data[0:4]) != "RIFF" {
		t.Errorf("output does not start with RIFF header")
	}
	if string(result.Data[8:12]) != "WAVE" {
		t.Errorf("output missing WAVE marker")
	}
	if result.SampleRate != 24000 {
		t.Errorf("expected 24000 Hz, got %d", result.SampleRate)
	}
	t.Logf("WAV: %d bytes, %d Hz, %d ch, %d-bit",
		len(result.Data), result.SampleRate, result.Channels, result.BitsPerSample)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustContain(t *testing.T, args []string, needle string) {
	t.Helper()
	for _, a := range args {
		if a == needle {
			return
		}
	}
	t.Errorf("args %v must contain %q", args, needle)
}

func pairMustExist(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("args %v must contain pair [%q, %q]", args, flag, value)
}

// makeWAVHeader builds a minimal valid 44-byte PCM WAV header.
func makeWAVHeader(sampleRate uint32, channels, bitsPerSample uint16) []byte {
	const dataSize = 0
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	putU32(h[4:], 36+dataSize)
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	putU32(h[16:], 16) // subchunk size
	putU16(h[20:], 1)  // PCM
	putU16(h[22:], channels)
	putU32(h[24:], sampleRate)
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	putU32(h[28:], byteRate)
	blockAlign := channels * bitsPerSample / 8
	putU16(h[32:], blockAlign)
	putU16(h[34:], bitsPerSample)
	copy(h[36:], "data")
	putU32(h[40:], dataSize)
	return h
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putU16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

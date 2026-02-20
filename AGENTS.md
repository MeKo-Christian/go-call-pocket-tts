# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`go-call-pocket-tts` is a Go library (package `pockettts`) that wraps the external `pocket-tts` CLI tool. It spawns `pocket-tts generate` as a subprocess, pipes text via stdin, and returns WAV audio bytes from stdout. There is no network server or HTTP client — it's purely a subprocess wrapper.

The external dependency (`pocket-tts`) is a Python-based TTS tool installed separately (e.g. via `uvx pocket-tts` or a dedicated Python venv).

## Commands

```bash
# Build
go build ./...

# Run tests
go test ./...

# Run a single test
go test -run TestName ./...

# Run tests with race detector
go test -race ./...

# Vet
go vet ./...
```

## Architecture

All files belong to package `pockettts` (single package, no subdirectories).

| File           | Role                                                                                                                                         |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `pockettts.go` | Public API: `Options`, `WAVResult`, `Client`, `Generate()`, `NewClient()`, `Preflight()`                                                     |
| `client.go`    | Internal: `newClient()`, `Client.generate()`, `buildArgs()`, `preflight()`                                                                   |
| `runner.go`    | Internal: `runner` struct that spawns/manages the subprocess (stdin write goroutine, stdout/stderr capture)                                  |
| `wav.go`       | Internal: `parseWAVHeader()` — validates RIFF/WAVE magic bytes and extracts audio metadata from the first 44 bytes                           |
| `errors.go`    | All error types: `ErrEmptyText`, `ErrExecutableNotFound`, `ErrProcessTimeout`, `ErrNonZeroExit`, `ErrInvalidVoice`, `ErrModelDownloadFailed` |
| `format.go`    | Helpers `formatFloat`/`formatInt` for CLI argument construction                                                                              |

### Data flow

```
caller → Client.generate()
           → input validation (ErrEmptyText)
           → concurrency semaphore (chan struct{})
           → buildArgs() → ["generate", "--text", "-", "--output-path", "-", ...]
           → runner.run(ctx, args, []byte(text))
               → exec.CommandContext
               → stdin write goroutine (avoids pipe buffer deadlock)
               → cmd.Wait()
               → error classification (not found / timeout / non-zero exit)
           → parseWAVHeader(stdout) → WAVResult{Data, SampleRate, Channels, BitsPerSample}
```

### Key design decisions

- **Zero values mean "use CLI defaults"** — all numeric `Options` fields are omitted from the CLI args when zero, so callers only need to set what they want to override.
- **Stdin goroutine**: stdin is written in a separate goroutine (`runner.go`) to prevent deadlock when the pipe buffer fills before the process starts reading.
- **Concurrency limiter**: `Client` holds an optional buffered channel (`sem`) acting as a semaphore; `Options.Concurrency <= 0` disables the limit.
- **Stderr handling**: stderr is always captured (last 512 bytes) for error reporting; optionally tee'd to `Options.LogWriter`. It is never mixed with stdout (WAV audio).
- **ErrProcessTimeout.Is()**: implements the `Is` method so `errors.Is(err, &ErrProcessTimeout{})` works correctly for pointer receivers.

### CLI flag mapping (Options → pocket-tts flags)

| Options field    | CLI flag                   |
| ---------------- | -------------------------- |
| `Voice`          | `--voice`                  |
| `Config`         | `--config`                 |
| `Temperature`    | `--temperature`            |
| `LSDDecodeSteps` | `--lsd-decode-steps`       |
| `NoiseClamp`     | `--noise-clamp`            |
| `EOSThreshold`   | `--eos-threshold`          |
| `FramesAfterEOS` | `--frames-after-eos`       |
| `MaxTokens`      | `--max-tokens`             |
| `Quiet`          | `--quiet`                  |
| (always)         | `--text -` (stdin)         |
| (always)         | `--output-path -` (stdout) |

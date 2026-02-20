# go-call-pocket-tts

A Go library (`package pockettts`) that wraps the [pocket-tts](https://github.com/kyutai-labs/pocket-tts) CLI tool — a high-quality, CPU-capable Text-to-Speech engine from Kyutai.

It supports two integration modes:

| Mode       | How it works                             | Best for                                          |
| ---------- | ---------------------------------------- | ------------------------------------------------- |
| **CLI**    | Spawns `pocket-tts generate` per request | Simplicity, correctness, low request rate         |
| **Server** | Connects to `pocket-tts serve` (HTTP)    | Low latency, high request rate (model stays warm) |

---

## Requirements

- Go 1.21+
- Python 3.10–3.14 with `pocket-tts` installed (see below)

---

## Installing pocket-tts

### Option A — uv (recommended, isolated)

```bash
uv tool install pocket-tts \
    --extra-index-url https://download.pytorch.org/whl/cpu \
    --index-strategy unsafe-best-match
```

### Option B — pip

```bash
pip install pocket-tts \
    --extra-index-url https://download.pytorch.org/whl/cpu \
    --index-strategy unsafe-best-match
```

### Option C — justfile (this repo)

```bash
just setup
```

Verify installation:

```bash
pocket-tts generate --help
```

> **Note:** The first generation downloads model weights (~300 MB) from Hugging Face into `$HF_HOME` (default: `~/.cache/huggingface`). Subsequent calls are fast because the cache is reused.

---

## Go API

### Install

```bash
go get github.com/MeKo-Christian/go-call-pocket-tts
```

### CLI mode — one-shot generate

```go
import (
    "context"
    "os"
    pockettts "github.com/MeKo-Christian/go-call-pocket-tts"
)

func main() {
    ctx := context.Background()

    result, err := pockettts.Generate(ctx, "Hello, world!", nil)
    if err != nil {
        panic(err)
    }
    os.WriteFile("out.wav", result.Data, 0o644)
}
```

### CLI mode — reusable client with options

```go
client := pockettts.NewClient(pockettts.Options{
    Voice:       "mimi",          // built-in voice or path to .safetensors
    Quiet:       true,
    Concurrency: 2,               // max 2 parallel subprocesses
    LogWriter:   os.Stderr,       // timing + stderr forwarded here
})

result, err := client.Generate(ctx, "Good morning.")
// result.Stats.Duration holds wall-clock time for this call
```

### Export a voice embedding (one-time offline step)

```go
err := pockettts.ExportVoice(ctx, "my_speaker.wav", "my_speaker.safetensors", nil)
// Then use: Options{Voice: "my_speaker.safetensors"}
```

### Server mode — warm model, low latency

```go
sc := pockettts.NewServerClient(pockettts.ServerOptions{
    Port:           8000,
    StartupTimeout: 5 * time.Minute, // allow model download on first start
    LogWriter:      os.Stderr,
})

// Start the pocket-tts server (blocks until /health responds).
if err := sc.Start(ctx); err != nil {
    panic(err)
}
defer sc.Stop()

result, err := sc.Generate(ctx, "Server mode is fast.", nil)

// Or with voice cloning via URL:
result, err = sc.Generate(ctx, "Custom voice.", &pockettts.ServerGenerateOptions{
    VoiceURL: "https://example.com/speaker.wav",
})

// Or upload a local voice file:
result, err = sc.Generate(ctx, "Custom voice.", &pockettts.ServerGenerateOptions{
    VoiceWAVPath: "my_speaker.wav",
})
```

### Preflight check

```go
if err := pockettts.Preflight(""); err != nil {
    log.Fatalf("pocket-tts not found: %v", err)
}
```

### Error handling

```go
var notFound *pockettts.ErrExecutableNotFound
var timeout  *pockettts.ErrProcessTimeout
var exitErr  *pockettts.ErrNonZeroExit

switch {
case errors.As(err, &notFound):
    // Install pocket-tts
case errors.As(err, &timeout):
    // Context deadline exceeded
case errors.As(err, &exitErr):
    fmt.Println("exit code:", exitErr.ExitCode)
    fmt.Println("stderr:", exitErr.Stderr)
case errors.Is(err, pockettts.ErrEmptyText):
    // Caller sent empty text
}
```

---

## Development

```bash
# Build
go build ./...

# Run tests (pocket-tts must be installed for golden tests)
go test ./...

# Run with race detector
go test -race ./...

# All checks (format + lint + test + mod tidy)
just ci
```

---

## Docker

### Single container (Go app + pocket-tts)

```bash
docker build -t my-tts-app .
docker run --rm -v tts-cache:/cache -e HF_HOME=/cache my-tts-app
```

### Sidecar pattern (separate containers)

```bash
docker compose up
```

See `docker-compose.yml` for the configuration. The Go service connects to the
pocket-tts sidecar at `http://pocket-tts:8000` using `ServerClient`.

---

## Troubleshooting

### `pocket-tts: executable not found`

The `pocket-tts` binary is not on your `$PATH`. Run `just setup` or check your
Python environment is activated.

### `torch` is missing / import errors

pocket-tts requires PyTorch 2.5+. Install with the CPU-only extra index URL
(see Installation above). GPU builds work too but are not required.

### Model download fails / hangs

- Check internet connectivity and that `huggingface.co` is reachable.
- Some voice models are gated — accept the license on Hugging Face and set
  `HF_TOKEN` (or `HUGGING_FACE_HUB_TOKEN`) in your environment.
- Set `HF_HOME` to a writable directory with enough disk space (~300 MB).

### High memory usage with CLI mode

Each `pocket-tts generate` subprocess loads the full model. Use
`Options.Concurrency` to cap parallel subprocesses, or switch to server mode
(`pocket-tts serve`) where the model is loaded once and stays in memory.

### First request is slow

The model is downloaded and loaded on first use. Subsequent calls reuse the
cached weights. In server mode, this startup cost is paid once at `Start()`.

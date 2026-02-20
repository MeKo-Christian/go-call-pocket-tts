## Phase 0 — Decide the “Reference Interface” (1–2 tasks)

1. **Choose output contract for Go callers**

- Deliverable: a short spec for your Go package API, e.g.:
  - input: `text`, `voice` (built-in name or file), optional `config`, generation params
  - output: `[]byte` WAV (or `io.Reader` stream), plus metadata (sample rate if you want)

2. **Pick initial invocation mode**

- Start with **CLI `generate`** for simplest correctness.
- Keep **server mode** as a later optimization option (same plan can accommodate both). ([PyPI][2])

---

## Phase 1 — Reproducible Runtime & Packaging (Foundation)

3. **Lock down how pocket-tts is installed**

- Option A: ship a dedicated Python venv alongside the Go service (recommended for stability).
- Option B: rely on `uvx pocket-tts …` for “on-demand isolated env” (great for dev/reference). ([PyPI][2])

4. **Create a “runtime preflight” checklist**

- Deliverable: a script/doc that verifies:
  - `pocket-tts` executable is resolvable
  - CPU-only PyTorch is available (pocket-tts requires PyTorch 2.5+ per package docs). ([PyPI][2])
  - model download/cache directory is writable (Hugging Face cache typically)

5. **Define caching policy (important for perf)**

- Deliverable: where model/voices cache lives in prod (container volume, writable dir, etc.)
- Goal: avoid re-downloading models across restarts.

---

## Phase 2 — Minimal Go Wrapper (Correctness first)

6. **Implement process runner abstraction**

- Deliverable: internal Go component that can:
  - spawn a process
  - stream stdin
  - read stdout/stderr concurrently
  - enforce timeout/cancellation

7. **Implement “generate” call (single request)**

- Use:
  - `pocket-tts generate --text "-" --output-path "-" …` ([GitHub][1])

- Deliverable: function that returns WAV bytes (stdout).

8. **Parameter mapping table**

- Deliverable: a table in your repo mapping your Go options → CLI flags:
  - `--voice`, `--config`, `--temperature`, `--lsd-decode-steps`, `--noise-clamp`, `--eos-threshold`, `--frames-after-eos`, `--max-tokens`, etc. ([GitHub][1])

9. **Stderr/logging handling**

- Decide: forward stderr to your logger, but never mix it into audio output.
- Use `--quiet` when you need clean logs (CLI supports `-q/--quiet`). ([GitHub][1])

---

## Phase 3 — Robustness & Operational Hardening

10. **Input validation + safety constraints**

- Tasks:
  - reject empty/whitespace-only text (same constraint exists on server endpoint; mirror it). ([GitHub][1])
  - cap max text length (you choose), and/or chunk long texts upstream

11. **Timeouts and cancellation**

- Deliverable: request-level timeout (context cancellation kills the subprocess)

12. **Resource limits**

- Tasks:
  - limit concurrency (each generate loads model unless you use server mode)
  - consider a worker pool to prevent CPU exhaustion

13. **Error taxonomy**

- Deliverable: structured error types:
  - “executable not found”
  - “model download failed”
  - “invalid voice”
  - “process timeout”
  - “non-zero exit + captured stderr excerpt”

14. **Golden tests**

- Deliverable: automated test that:
  - runs a short text
  - asserts output starts with WAV header (`RIFF...WAVE`)
  - stores a small golden WAV checksum (be careful with nondeterminism if temperature > 0)

---

## Phase 4 — Performance Iteration (Still Empfehlung A, but better)

15. **Voice-state speedups (optional but high ROI)**

- pocket-tts provides `export-voice` to convert an audio prompt into a `.safetensors` “voice embedding” that loads fast. ([PyPI][2])
- Deliverable:
  - an offline step (or admin command) to export voices you’ll reuse
  - production config uses `--voice path/to/voice.safetensors` instead of raw WAV

16. **Warm model option (server mode)**

- Instead of reloading per call, run `pocket-tts serve` once and call `POST /tts` from Go. The server streams `audio/wav`. ([GitHub][1])
- Deliverables:
  - a sidecar/service mode that starts the server on localhost
  - Go client that posts `text` + `voice_url` or uploads `voice_wav`
  - health checks against `/health` ([GitHub][1])

- Decision point: keep CLI mode as fallback/reference even if you adopt server mode.

---

## Phase 5 — Deployment & DX

17. **Containerization approach**

- Deliverable: one of:
  - single container (Go + Python env + pocket-tts installed)
  - dual container (Go service + pocket-tts server sidecar)

18. **Observability**

- Add metrics:
  - TTS latency (spawn time vs generation time)
  - process exit codes
  - cache hit/miss indicators (best-effort via logs)
  - concurrency/queue depth

19. **Documentation**

- Deliverable: “How to run locally” with:
  - `uvx pocket-tts generate …` reference command ([PyPI][2])
  - expected Go API usage
  - troubleshooting section (missing torch, cache permissions, etc.)

---

## Phase 6 — “Gate” to move beyond Empfehlung A

20. **Acceptance checklist (to declare A done)**

- Correctness: WAV output valid, voices selectable, errors surfaced cleanly
- Operability: reproducible install, logs/metrics, timeouts
- Baseline performance: documented numbers for your target CPU

21. **Handoff artifacts for “Empfehlung B (native)”**

- Deliverable: a small benchmark harness + sample inputs/voices that you can reuse to validate any native/ONNX approach later.

---

If you want, I can also turn this into a **Jira/Linear-friendly task list** (with estimates, dependencies, and clear acceptance criteria per task) while keeping it code-free.

[1]: https://raw.githubusercontent.com/kyutai-labs/pocket-tts/main/pocket_tts/main.py "raw.githubusercontent.com"
[2]: https://pypi.org/project/pocket-tts/ "pocket-tts · PyPI"

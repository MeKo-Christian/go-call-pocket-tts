# Multi-stage Dockerfile for a Go application that embeds pocket-tts.
#
# This builds a single image containing:
#   - A compiled Go binary (your application, not this library)
#   - Python + pocket-tts (CPU-only PyTorch)
#
# Usage:
#   docker build -t my-tts-app .
#   docker run --rm -e HF_HOME=/cache -v tts-cache:/cache my-tts-app
#
# Note: The first run downloads model weights (~300 MB) into HF_HOME.
#       Mount a persistent volume there to avoid re-downloading.

# ─── Stage 1: Go build ───────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build the example binary if present, otherwise just verify the library compiles.
RUN go build ./...


# ─── Stage 2: Runtime (Python + pocket-tts + Go binary) ──────────────────────
FROM python:3.12-slim-bookworm AS runtime

# Install uv for fast, isolated Python package management.
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# Install pocket-tts with CPU-only PyTorch (no CUDA drivers needed).
# --system installs into the system Python so pocket-tts is on PATH.
RUN uv pip install --system pocket-tts \
        --extra-index-url https://download.pytorch.org/whl/cpu \
        --index-strategy unsafe-best-match

# Verify the binary is accessible.
RUN which pocket-tts

# Copy Go binary from the builder stage.
# Replace "myapp" with your actual binary name.
# COPY --from=go-builder /src/myapp /usr/local/bin/myapp

# Hugging Face model cache — mount a volume here in production.
ENV HF_HOME=/cache
VOLUME ["/cache"]

# Run your Go application.
# CMD ["/usr/local/bin/myapp"]

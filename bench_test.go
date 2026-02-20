package pockettts

// Benchmark harness for CLI vs server-mode latency comparison.
//
// Both benchmarks are skipped when pocket-tts is not on PATH.
// Run with:
//
//	go test -bench=. -benchtime=3x -timeout=30m ./...
//
// The results serve as a baseline for comparing against a native Go
// implementation (Empfehlung B / go-pocket-tts).

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

const benchText = "The quick brown fox jumps over the lazy dog."

func BenchmarkCLIGenerate(b *testing.B) {
	if _, err := exec.LookPath("pocket-tts"); err != nil {
		b.Skip("pocket-tts not found on PATH")
	}

	ctx := context.Background()
	client := NewClient(Options{Quiet: true})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := client.Generate(ctx, benchText)
		if err != nil {
			b.Fatalf("Generate: %v", err)
		}
		b.ReportMetric(float64(result.Stats.Duration.Milliseconds()), "ms/op")
		b.SetBytes(int64(len(result.Data)))
	}
}

func BenchmarkServerGenerate(b *testing.B) {
	if _, err := exec.LookPath("pocket-tts"); err != nil {
		b.Skip("pocket-tts not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sc := NewServerClient(ServerOptions{
		Port:           18766,
		StartupTimeout: 5 * time.Minute,
	})
	if err := sc.Start(ctx); err != nil {
		b.Fatalf("Start: %v", err)
	}
	defer sc.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := sc.Generate(ctx, benchText, nil)
		if err != nil {
			b.Fatalf("Generate: %v", err)
		}
		b.ReportMetric(float64(result.Stats.Duration.Milliseconds()), "ms/op")
		b.SetBytes(int64(len(result.Data)))
	}
}

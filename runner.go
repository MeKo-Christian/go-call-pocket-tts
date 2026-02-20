package pockettts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// runResult holds the captured output of a subprocess run.
type runResult struct {
	stdout []byte
	stderr string // last portion captured for error reporting
}

// runner spawns a single pocket-tts subprocess, writes text to its stdin,
// and reads WAV bytes from its stdout, while draining stderr concurrently.
type runner struct {
	executablePath string
	logWriter      io.Writer
}

func (r *runner) run(ctx context.Context, args []string, stdinPayload []byte) (*runResult, error) {
	exe := r.executablePath
	if exe == "" {
		exe = "pocket-tts"
	}

	cmd := exec.CommandContext(ctx, exe, args...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("pockettts: create stdin pipe: %w", err)
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	// stderr is always captured; also tee to logWriter if set.
	if r.logWriter != nil {
		cmd.Stderr = io.MultiWriter(&stderrBuf, r.logWriter)
	} else {
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Start(); err != nil {
		if isNotFound(err) {
			return nil, &ErrExecutableNotFound{Executable: exe}
		}
		return nil, fmt.Errorf("pockettts: start process: %w", err)
	}

	// Write stdin in a goroutine so we don't deadlock if the pipe buffer fills.
	var wg sync.WaitGroup
	var stdinErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stdinPipe.Close()
		_, stdinErr = stdinPipe.Write(stdinPayload)
	}()

	wg.Wait()
	if stdinErr != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("pockettts: write stdin: %w", stdinErr)
	}

	waitErr := cmd.Wait()

	res := &runResult{
		stdout: stdoutBuf.Bytes(),
		stderr: truncate(stderrBuf.String(), 512),
	}

	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, &ErrProcessTimeout{Stderr: res.stderr}
		}
		return nil, &ErrNonZeroExit{
			ExitCode: cmd.ProcessState.ExitCode(),
			Stderr:   res.stderr,
		}
	}

	return res, nil
}

// truncate keeps at most n bytes from the end of s (for stderr excerpts).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

package pockettts

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Sentinel errors you can compare with errors.Is.
var (
	// ErrEmptyText is returned when the provided text is empty or whitespace-only.
	ErrEmptyText = errors.New("pockettts: text must not be empty")
)

// ErrExecutableNotFound is returned when the pocket-tts binary cannot be located.
type ErrExecutableNotFound struct {
	Executable string
}

func (e *ErrExecutableNotFound) Error() string {
	return fmt.Sprintf("pockettts: executable not found: %q (install pocket-tts or set ExecutablePath)", e.Executable)
}

// ErrProcessTimeout is returned when the context deadline is exceeded while
// waiting for the pocket-tts process.
type ErrProcessTimeout struct {
	Stderr string
}

func (e *ErrProcessTimeout) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("pockettts: process timed out; stderr: %s", e.Stderr)
	}
	return "pockettts: process timed out"
}

func (e *ErrProcessTimeout) Is(target error) bool {
	_, ok := target.(*ErrProcessTimeout)
	return ok
}

// ErrNonZeroExit is returned when the pocket-tts process exits with a non-zero
// status code.
type ErrNonZeroExit struct {
	ExitCode int
	Stderr   string
}

func (e *ErrNonZeroExit) Error() string {
	return fmt.Sprintf("pockettts: process exited with code %d; stderr: %s", e.ExitCode, e.Stderr)
}

// ErrInvalidVoice is returned when the CLI reports that the requested voice is
// unknown or the voice file cannot be loaded.
type ErrInvalidVoice struct {
	Voice string
}

func (e *ErrInvalidVoice) Error() string {
	return fmt.Sprintf("pockettts: invalid voice: %q", e.Voice)
}

// ErrModelDownloadFailed is returned when the CLI appears to fail during model
// or weight download (detected heuristically from stderr).
type ErrModelDownloadFailed struct {
	Stderr string
}

func (e *ErrModelDownloadFailed) Error() string {
	return fmt.Sprintf("pockettts: model download failed; stderr: %s", e.Stderr)
}

// isNotFound reports whether err indicates the executable was not found.
func isNotFound(err error) bool {
	// exec.LookPath failure (no absolute path given)
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	// Absolute-path case: os.PathError with ENOENT
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return errors.Is(pathErr.Err, syscall.ENOENT)
	}
	return false
}

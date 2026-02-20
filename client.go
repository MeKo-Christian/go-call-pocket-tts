package pockettts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cwbudde/wav"
)

// newClient constructs a client from options (internal).
func newClient(opts *Options) *Client {
	c := &Client{opts: *opts}
	if opts.Concurrency > 0 {
		c.sem = make(chan struct{}, opts.Concurrency)
	}
	return c
}

// generate is the core implementation shared by Client.Generate and the
// package-level Generate function.
func (c *Client) generate(ctx context.Context, text string) (*WAVResult, error) {
	// Input validation
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyText
	}

	// Concurrency limiter: acquire slot (blocks until one is free or ctx is done).
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			return nil, &ErrProcessTimeout{Stderr: "context cancelled while waiting for concurrency slot"}
		}
	}

	args := c.buildArgs()

	r := &runner{
		executablePath: c.opts.ExecutablePath,
		logWriter:      c.opts.LogWriter,
	}

	res, err := r.run(ctx, args, []byte(text))
	if err != nil {
		return nil, err
	}

	if len(res.stdout) == 0 {
		return nil, &ErrNonZeroExit{ExitCode: 0, Stderr: "empty stdout — no WAV produced"}
	}

	sr, ch, bps, err := parseWAVHeader(res.stdout)
	if err != nil {
		return nil, err
	}

	return &WAVResult{
		Data:          res.stdout,
		SampleRate:    sr,
		Channels:      ch,
		BitsPerSample: bps,
	}, nil
}

// parseWAVHeader reads WAV metadata from data and returns format information.
func parseWAVHeader(data []byte) (sampleRate uint32, channels uint16, bitsPerSample uint16, err error) {
	if len(data) == 0 {
		return 0, 0, 0, fmt.Errorf("pockettts: WAV output too short (%d bytes)", len(data))
	}

	dec := wav.NewDecoder(bytes.NewReader(data))
	dec.ReadInfo()
	if dec.Err() != nil {
		return 0, 0, 0, fmt.Errorf("pockettts: output is not a readable WAV file: %w", dec.Err())
	}

	if dec.SampleRate == 0 || dec.NumChans == 0 || dec.BitDepth == 0 {
		return 0, 0, 0, fmt.Errorf("pockettts: output WAV metadata is incomplete")
	}

	return dec.SampleRate, dec.NumChans, dec.BitDepth, nil
}

// buildArgs constructs the CLI argument slice for `pocket-tts generate`.
//
// Mapping table:
//
//	Options.Voice          → --voice <value>
//	Options.Config         → --config <value>
//	Options.Temperature    → --temperature <value>   (only if != 0)
//	Options.LSDDecodeSteps → --lsd-decode-steps <n>  (only if != 0)
//	Options.NoiseClamp     → --noise-clamp <value>   (only if != 0)
//	Options.EOSThreshold   → --eos-threshold <value> (only if != 0)
//	Options.FramesAfterEOS → --frames-after-eos <n>  (only if != 0)
//	Options.MaxTokens      → --max-tokens <n>         (only if != 0)
//	Options.Quiet          → --quiet
//	stdin                  → --text -
//	stdout                 → --output-path -
func (c *Client) buildArgs() []string {
	args := []string{
		"generate",
		"--text", "-",
		"--output-path", "-",
	}

	if c.opts.Voice != "" {
		args = append(args, "--voice", c.opts.Voice)
	}
	if c.opts.Config != "" {
		args = append(args, "--config", c.opts.Config)
	}
	if c.opts.Temperature != 0 {
		args = append(args, "--temperature", formatFloat(c.opts.Temperature))
	}
	if c.opts.LSDDecodeSteps != 0 {
		args = append(args, "--lsd-decode-steps", formatInt(c.opts.LSDDecodeSteps))
	}
	if c.opts.NoiseClamp != 0 {
		args = append(args, "--noise-clamp", formatFloat(c.opts.NoiseClamp))
	}
	if c.opts.EOSThreshold != 0 {
		args = append(args, "--eos-threshold", formatFloat(c.opts.EOSThreshold))
	}
	if c.opts.FramesAfterEOS != 0 {
		args = append(args, "--frames-after-eos", formatInt(c.opts.FramesAfterEOS))
	}
	if c.opts.MaxTokens != 0 {
		args = append(args, "--max-tokens", formatInt(c.opts.MaxTokens))
	}
	if c.opts.Quiet {
		args = append(args, "--quiet")
	}

	return args
}

// preflight checks that the pocket-tts executable is resolvable.
func preflight(executablePath string) error {
	exe := executablePath
	if exe == "" {
		exe = "pocket-tts"
	}
	_, err := exec.LookPath(exe)
	if err != nil {
		return &ErrExecutableNotFound{Executable: exe}
	}
	return nil
}

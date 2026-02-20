package pockettts

import (
	"context"
	"fmt"
)

// exportVoice is the internal implementation of ExportVoice.
func exportVoice(ctx context.Context, audioPath, exportPath string, opts *ExportVoiceOptions) error {
	if audioPath == "" {
		return fmt.Errorf("pockettts: audioPath must not be empty")
	}
	if exportPath == "" {
		return fmt.Errorf("pockettts: exportPath must not be empty")
	}

	args := []string{"export-voice", audioPath, exportPath}
	if opts.Config != "" {
		args = append(args, "--config", opts.Config)
	}
	if opts.Quiet {
		args = append(args, "--quiet")
	}

	r := &runner{
		executablePath: opts.ExecutablePath,
		logWriter:      opts.LogWriter,
	}

	_, err := r.run(ctx, args, nil)
	return err
}

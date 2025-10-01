package efrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"

	"github.com/sa6mwa/emrun"
	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"github.com/sa6mwa/emrun/port"
)

type Background = emrun.Background
type Result = emrun.Result

type runnable struct {
	payload       []byte
	file          *os.File
	name          string
	sha256hex     string
	sha256        [32]byte
	deleteOnClose bool
	runner        port.CommandRunner
}

func (r *runnable) Name() string {
	return r.name
}

func (r *runnable) IsMemfd() bool {
	return false
}

func (r *runnable) ensureDigest() ([32]byte, string) {
	if r.sha256hex != "" {
		return r.sha256, r.sha256hex
	}
	sum := sha256.Sum256(r.payload)
	r.sha256 = sum
	r.sha256hex = hex.EncodeToString(sum[:])
	return r.sha256, r.sha256hex
}

func (r *runnable) enforce(ctx context.Context) error {
	digest, hexDigest := r.ensureDigest()
	return emrun.CheckPolicy(ctx, digest, hexDigest)
}

func (r *runnable) Close() error {
	var fileCloseErr error
	if r.file != nil {
		fileCloseErr = r.file.Close()
		r.file = nil
	}
	if r.deleteOnClose && r.name != "" {
		if err := os.Remove(r.name); err != nil {
			if fileCloseErr != nil {
				return fmt.Errorf("close error: %w; remove error: %w", fileCloseErr, err)
			}
			return err
		}
		r.deleteOnClose = false
	}
	return fileCloseErr
}

func (r *runnable) Read(p []byte) (int, error) {
	if r.file == nil {
		return 0, os.ErrInvalid
	}
	return r.file.Read(p)
}

func (r *runnable) Seek(offset int64, whence int) (int64, error) {
	if r.file == nil {
		return 0, os.ErrInvalid
	}
	return r.file.Seek(offset, whence)
}

func (r *runnable) Run(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	if r.runner == nil {
		r.runner = commandrunner.Default
	}
	if err := r.enforce(ctx); err != nil {
		return nil, err
	}
	return emrun.RunCommand(r.runner, cmd, combinedOutput)
}

func (r *runnable) StartBackground(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) (*exec.Cmd, port.CommandCapture, error) {
	if r.runner == nil {
		r.runner = commandrunner.Default
	}
	if err := r.enforce(ctx); err != nil {
		return nil, nil, err
	}
	capture, err := emrun.StartCommand(r.runner, cmd, combinedOutput)
	if err != nil {
		return nil, nil, err
	}
	return cmd, capture, nil
}

package port

import (
	"context"
	"io"
	"os/exec"
)

// Runnable describes the minimal executable payload contract shared by emrun
// and efrun. Implementations manage lifecycle, expose an executable path, and
// provide a low-level Run helper used by the exported helpers.
type Runnable interface {
	io.Closer
	io.Reader
	io.Seeker
	Name() string
	IsMemfd() bool
	Run(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error)
}

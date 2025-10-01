// The emrun companion package efrun exposes the same API surface as emrun but
// always executes from a temporary file, which makes it portable to platforms
// where memfd_create is unavailable, but you have to explicitly choose that
// import.
package efrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/sa6mwa/emrun"
	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"github.com/sa6mwa/emrun/port"
)

var (
	ERR_PAYLOAD_IS_EMPTY error = errors.New("payload is empty")
)

// Open writes executablePayload to a temporary executable on disk and
// returns a runnable handle whose Name points at the file. The file
// is chmod +x and will be removed when Close is called. Payloads can
// be anything Linux can execute, such as ELF binaries or shebang
// scripts. Example:
//
//	//go:embed myapp
//	var elfOrShebangScript []byte
//	//...
//	f, err := efrun.Open(elfOrShebangScript)
//	if err != nil {
//		panic(err)
//	}
//	defer f.Close()
//	cmd := exec.Command(f.Name(), "--version")
//	//...
//	cmd.Run()
func Open(executablePayload []byte) (port.Runnable, error) {
	if len(executablePayload) == 0 {
		return nil, ERR_PAYLOAD_IS_EMPTY
	}
	sum := sha256.Sum256(executablePayload)
	r := &runnable{
		payload:       executablePayload,
		sha256hex:     hex.EncodeToString(sum[:]),
		sha256:        sum,
		deleteOnClose: true,
		runner:        commandrunner.Default,
	}
	if err := r.writeToTemporaryFile(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *runnable) writeToTemporaryFile() error {
	tmpf, err := os.CreateTemp("", r.sha256hex+"-*")
	if err != nil {
		return err
	}
	name := tmpf.Name()
	cleanup := true
	defer func() {
		if cleanup {
			tmpf.Close()
			os.Remove(name)
		}
	}()
	if _, err := tmpf.Write(r.payload); err != nil {
		return fmt.Errorf("unable to write to temporary file: %w", err)
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Chmod(name, 0o700); err != nil {
		return fmt.Errorf("chmod +x: %w", err)
	}
	rf, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("reopen temporary file: %w", err)
	}
	r.file = rf
	r.name = name
	cleanup = false
	return nil
}

// Run executes the payload with ctx in exec.CommandContext with args
// using (*exec.Cmd).CombinedOutput, returns combined output or
// error. cmd.Stdin is nil, use RunIO if you want to pass data via
// stdin.
func Run(ctx context.Context, executablePayload []byte, arg ...string) ([]byte, error) {
	f, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	runnable := f.(*runnable)
	cmd := exec.CommandContext(ctx, runnable.Name(), arg...)
	return runnable.Run(ctx, cmd, true)
}

// RunIO is similar to Run but uses r for stdin and w for stdout and
// stderr. Uses ctx for (*exec.Cmd).CommandContext.
func RunIO(ctx context.Context, r io.Reader, w io.Writer, executablePayload []byte, arg ...string) error {
	f, err := Open(executablePayload)
	if err != nil {
		return err
	}
	defer f.Close()
	runnable := f.(*runnable)
	cmd := exec.CommandContext(ctx, runnable.Name(), arg...)
	cmd.Stdin = r
	cmd.Stdout = w
	cmd.Stderr = w
	_, err = runnable.Run(ctx, cmd, false)
	return err
}

// RunIOE is exactly like RunIO except with separate stdout and stderr
// writers.
func RunIOE(ctx context.Context, r io.Reader, stdout io.Writer, stderr io.Writer, executablePayload []byte, arg ...string) error {
	f, err := Open(executablePayload)
	if err != nil {
		return err
	}
	defer f.Close()
	runnable := f.(*runnable)
	cmd := exec.CommandContext(ctx, runnable.Name(), arg...)
	cmd.Stdin = r
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_, err = runnable.Run(ctx, cmd, false)
	return err
}

// Do is intended to run shebang scripts inline or from string
// vars. Uses ctx in exec.CommandContext and returns
// (*exec.Cmd).CombinedOutput.
func Do(ctx context.Context, payload string, arg ...string) ([]byte, error) {
	return Run(ctx, []byte(payload), arg...)
}

// RunBG mirrors emrun.RunBG but always executes from a temporary file. The
// Background handle allows waiting on completion via select. Example:
//
//	bg, err := efrun.RunBG(ctx, payload, "--flag")
//	if err != nil {
//		return err
//	}
//	defer bg.Cancel()
//	select {
//	case res := <-bg.Done:
//		if res.Error != nil {
//			return res.Error
//		}
//	case <-ctx.Done():
//		return ctx.Err()
//	}
func RunBG(ctx context.Context, executablePayload []byte, arg ...string) (*Background, error) {
	r, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	return emrun.StartBackground(ctx, r.(*runnable), arg, nil, nil, nil, true)
}

// RunIOBG streams stdin/stdout/stderr via reader/writer while running in the
// background. Combined output in the Result is nil because output is streamed.
func RunIOBG(ctx context.Context, r io.Reader, w io.Writer, executablePayload []byte, arg ...string) (*Background, error) {
	run, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	return emrun.StartBackground(ctx, run.(*runnable), arg, r, w, w, false)
}

// RunIOEBG provides distinct stdout and stderr writers for background runs.
func RunIOEBG(ctx context.Context, r io.Reader, stdout io.Writer, stderr io.Writer, executablePayload []byte, arg ...string) (*Background, error) {
	run, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	return emrun.StartBackground(ctx, run.(*runnable), arg, r, stdout, stderr, false)
}

// DoBG runs the inline script in the background, returning a handle identical
// to RunBG for lifecycle management.
func DoBG(ctx context.Context, payload string, arg ...string) (*Background, error) {
	return RunBG(ctx, []byte(payload), arg...)
}

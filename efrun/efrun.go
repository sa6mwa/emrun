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

	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"github.com/sa6mwa/emrun/port"
)

var (
	ERR_PAYLOAD_IS_EMPTY error = errors.New("payload is empty")
)

type runnable struct {
	payload       []byte
	file          *os.File
	name          string
	sha256hex     string
	deleteOnClose bool
	runner        port.CommandRunner
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *runnable) Name() string {
	return r.name
}

func (r *runnable) IsMemfd() bool {
	return false
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

func (r *runnable) Run(_ context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	if r.runner == nil {
		r.runner = commandrunner.Default
	}
	return r.runner.Run(cmd, combinedOutput)
}

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
	r := &runnable{
		payload:       executablePayload,
		sha256hex:     sha256hex(executablePayload),
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

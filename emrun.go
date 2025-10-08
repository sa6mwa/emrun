//go:build linux || android
// +build linux android

// Run embedded executables and scripts straight from anonymous memory on Linux
// and Android. emrun wraps memfd_create(2) so you can bundle auxiliary tooling
// and scripts inside a Go binary, execute them without touching disk in the
// common case, and keep the package fully self-contained even when hardened
// kernels restrict anonymous execution.
package emrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
	"pkt.systems/emrun/adapters/commandrunner"
	"pkt.systems/emrun/port"
)

type Runnable = port.Runnable

var (
	ERR_PAYLOAD_IS_EMPTY   error = errors.New("payload is empty")
	ERR_NOT_AN_INMEMORY_FD error = errors.New("not an in-memory file descriptor")
)

// Open attempts to create a memory file descriptor using
// memfd_create(2), name will be a sha256 hash of the payload that
// will show up under /proc/<pid>/{fd,fdinfo}, running process will
// show up as /proc/self/fd/<int>. Returns an open file descriptor (or
// error) where Name() can be used to execute it. Close the fd when
// done. If unix.MemfdCreate() fails, Open will fall back to writing
// the payload as a temporary file with user execute bit set. When
// Close() is called, the temporary file will be deleted. Payload can
// be anything Linux/Android can execute (ELF and script
// shebang). Example:
//
//	//go:embed myapp
//	var elfOrShebangScript []byte
//	//...
//	f, err := emrun.Open("myapp", elfOrShebangScript)
//	if err != nil {
//		panic(err)
//	}
//	defer f.Close()
//	cmd := exec.Command(f.Name(), "--version")
//	//...
//	cmd.Run()
func Open(executablePayload []byte) (Runnable, error) {
	sum := sha256.Sum256(executablePayload)
	r := &runnable{
		payload:   executablePayload,
		sha256hex: hex.EncodeToString(sum[:]),
		sha256:    sum,
		runner:    commandrunner.Default,
	}
	fd, err := unix.MemfdCreate(r.sha256hex, 0)
	if err != nil {
		// unable to create ananoymous file, dump it as a temporary file instead
		if err := r.switchToTemporaryFile(); err != nil {
			return nil, err
		}
		// returns a runnable (actual file descriptor is closed; tempfile deleted on Close())
		return r, nil
	}
	// memfd_create(2) succeeded
	r.name = fmt.Sprintf("/proc/self/fd/%d", fd)
	f := os.NewFile(uintptr(fd), r.name)
	r.file = f
	r.closer = f
	r.deleteOnClose = false // nothing to delete (in-memory file)
	if _, err := r.file.Write(executablePayload); err != nil {
		if cerr := r.Close(); cerr != nil {
			return nil, fmt.Errorf("unable to write payload: %w; unable to close memfd: %w", err, cerr)
		}
		return nil, fmt.Errorf("unable to write payload: %w", err)
	}
	// return a runnable; memfd is open, gets closed on Close() (not deleted)
	return r, nil
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
	f, err := Open([]byte(payload))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	runnable := f.(*runnable)
	cmd := exec.CommandContext(ctx, runnable.Name(), arg...)
	return runnable.Run(ctx, cmd, true)
}

// RunBG launches the payload in the background and returns a Background handle
// that exposes the running context. Example usage:
//
//	bg, err := emrun.RunBG(ctx, payload, "--flag")
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
	return StartBackground(ctx, r.(*runnable), arg, nil, nil, nil, true)
}

// RunIOBG behaves like RunBG but wires the provided reader/writer to stdin and
// combined stdout/stderr. The returned Result has a nil CombinedOutput since
// output is streamed to writer.
func RunIOBG(ctx context.Context, reader io.Reader, writer io.Writer, executablePayload []byte, arg ...string) (*Background, error) {
	r, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	return StartBackground(ctx, r.(*runnable), arg, reader, writer, writer, false)
}

// RunIOEBG is the background variant of RunIOE, streaming stdout and stderr to
// separate writers while returning a Background handle for lifecycle control.
func RunIOEBG(ctx context.Context, reader io.Reader, stdout io.Writer, stderr io.Writer, executablePayload []byte, arg ...string) (*Background, error) {
	r, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	return StartBackground(ctx, r.(*runnable), arg, reader, stdout, stderr, false)
}

// DoBG runs the provided script string in the background, mirroring Do but
// returning a Background handle so callers can select on completion or cancel.
func DoBG(ctx context.Context, payload string, arg ...string) (*Background, error) {
	return RunBG(ctx, []byte(payload), arg...)
}

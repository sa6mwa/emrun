//go:build linux || android

package emrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"golang.org/x/sys/unix"
)

var (
	ERR_PAYLOAD_IS_EMPTY   error = errors.New("payload is empty")
	ERR_NOT_AN_INMEMORY_FD error = errors.New("not an in-memory file descriptor")
)

type Runnable interface {
	io.Closer
	io.Reader
	io.Seeker
	Name() string
	IsMemfd() bool
}

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
//	cmd := exec.Command(f.Name(), "--version")
//	//...
//	cmd.Run()
func Open(executablePayload []byte) (Runnable, error) {
	r := &runnable{
		payload:   executablePayload,
		sha256hex: sha256hex(executablePayload),
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
			return nil, fmt.Errorf("unable to write payload: %w, unable to close memfd: %w", err, cerr)
		}
		return nil, fmt.Errorf("unable to write payload: %w", err)
	}
	// return a runnable; memfd is open, gets closed on Close() (not deleted)
	return r, nil
}

// Run executes the payload with ctx in exec.CommandContext with args
// using (*exec.Cmd).CombinedOutput, returns combined output or error.
func Run(ctx context.Context, executablePayload []byte, arg ...string) ([]byte, error) {
	f, err := Open(executablePayload)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := f.(*runnable)
	cmd := exec.CommandContext(ctx, r.Name(), arg...)
	return r.run(ctx, cmd, true)
}

// RunIO is similar to Run but uses r for stdin and w for stdout and
// stderr (unless nil, then it defaults to os.Stdin, os.Stdout and
// os.Stderr respectively). Uses ctx for (*exec.Cmd).CommandContext.
func RunIO(ctx context.Context, r io.Reader, w io.Writer, executablePayload []byte, arg ...string) error {
	f, err := Open(executablePayload)
	if err != nil {
		return err
	}
	defer f.Close()
	runnable := f.(*runnable)
	cmd := exec.CommandContext(ctx, runnable.Name(), arg...)
	if r != nil {
		cmd.Stdin = r
	} else {
		cmd.Stdin = os.Stdin
	}
	if w != nil {
		cmd.Stdout = w
		cmd.Stderr = w
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	_, err = runnable.run(ctx, cmd, false)
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
	r := f.(*runnable)
	cmd := exec.CommandContext(ctx, r.Name(), arg...)
	return r.run(ctx, cmd, true)
}

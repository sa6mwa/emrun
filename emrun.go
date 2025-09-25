//go:build linux || android

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
	"strings"

	"golang.org/x/sys/unix"
)

var (
	ERR_PAYLOAD_IS_EMPTY   error = errors.New("payload is empty")
	ERR_NOT_AN_INMEMORY_FD error = errors.New("not an in-memory file descriptor")
)

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type Runnable interface {
	io.Closer
	Name() string
	//Stat() (fs.FileInfo, error)
	IsMemfd() bool
}

type runnable struct {
	payload       []byte
	file          *os.File
	closer        io.Closer
	name          string
	sha256hex     string
	deleteOnClose bool
}

func (r *runnable) IsMemfd() bool {
	if !strings.HasPrefix("/proc/self/fd", r.name) {
		return true
	}
	return false
}

func (r *runnable) switchToTemporaryFile() error {
	if !r.IsMemfd() {
		return ERR_NOT_AN_INMEMORY_FD
	}
	if len(r.payload) == 0 {
		return ERR_PAYLOAD_IS_EMPTY
	}
	// Close any previous instance
	r.Close()
	if r.sha256hex == "" {
		r.sha256hex = sha256hex(r.payload)
	}
	tmpf, err := os.CreateTemp("", r.sha256hex+"-*")
	if err != nil {
		return err
	}
	r.file = tmpf
	r.closer = tmpf
	r.name = tmpf.Name()
	r.deleteOnClose = true
	if _, err := r.file.Write(r.payload); err != nil {
		if cerr := r.Close(); cerr != nil {
			return fmt.Errorf("unable to write to temporary file: %w, unable to close temporary file: %w", err, cerr)
		}
		return fmt.Errorf("unable to write to temporary file: %w", err)
	}
	// Clsoe underlying tempfile
	r.file.Close()
	r.closer = nil
	if err := os.Chmod(r.name, 0o0700); err != nil {
		if cerr := r.Close(); cerr != nil {
			return fmt.Errorf("unable to chmod temporary file: %w, unable to close temporary file: %w", err, cerr)
		}
		return fmt.Errorf("chmod +x: %w", err)
	}
	return nil
}

func (r *runnable) Name() string {
	if r.name == "" && r.file != nil {
		return r.file.Name()
	}
	return r.name
}

func (r *runnable) Close() error {
	var fileCloseErr error
	if r.file != nil && r.closer != nil {
		fileCloseErr = r.file.Close()
		r.closer = nil
	}
	if r.deleteOnClose {
		if err := os.Remove(r.name); err != nil {
			if fileCloseErr != nil {
				return fmt.Errorf("close error: %w, remove error: %w", fileCloseErr, err)
			}
			return err
		}
		r.deleteOnClose = false
	}
	return fileCloseErr
}

func (r *runnable) run(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	var b []byte
	var err error
	if combinedOutput {
		b, err = cmd.CombinedOutput()
	} else {
		err = cmd.Run()
	}
	if err != nil {
		if !r.IsMemfd() {
			return nil, err
		}
		// if error is permission denied, something indicating that
		// the command was not actually run because the OS said no, we
		// need to catch it here. If it's a permission issue and not a return or exit code from the execution,
		// then do switch to tempfile (as it's a memfd according at this point according to the check above)...

		// if err := switchToTemporaryFile(); err != nil {}

	}
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
		if cerr := r.Close(); err != nil {
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
	return exec.CommandContext(ctx, f.Name(), arg...).CombinedOutput()
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
	cmd := exec.CommandContext(ctx, f.Name(), arg...)
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

	if err := cmd.Run(); err != nil {
	}

	return cmd.Run()
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
	return exec.CommandContext(ctx, f.Name(), arg...).CombinedOutput()
}

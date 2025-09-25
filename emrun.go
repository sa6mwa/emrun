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
	"slices"
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
	io.Reader
	io.Seeker
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
	return strings.HasPrefix(r.name, "/proc/self/fd/")
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

func (r *runnable) run(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	runOnce := func(c *exec.Cmd) ([]byte, error) {
		if combinedOutput {
			return c.CombinedOutput()
		}
		return nil, c.Run()
	}
	b, err := runOnce(cmd)
	if err == nil {
		return b, nil
	}
	if !r.IsMemfd() {
		return b, err
	}
	isPermissionErr := func(runErr error) bool {
		if errors.Is(runErr, os.ErrPermission) {
			return true
		}
		var pathErr *os.PathError
		if errors.As(runErr, &pathErr) {
			return errors.Is(pathErr.Err, os.ErrPermission) || errors.Is(pathErr.Err, unix.EACCES) || errors.Is(pathErr.Err, unix.EPERM)
		}
		var execErr *exec.Error
		if errors.As(runErr, &execErr) {
			return errors.Is(execErr.Err, os.ErrPermission) || errors.Is(execErr.Err, unix.EACCES) || errors.Is(execErr.Err, unix.EPERM)
		}
		return errors.Is(runErr, unix.EACCES) || errors.Is(runErr, unix.EPERM)
	}

	if !isPermissionErr(err) {
		return b, err
	}

	if serr := r.switchToTemporaryFile(); serr != nil {
		return b, fmt.Errorf("memfd execution failed: %w; fallback to tempfile failed: %w", err, serr)
	}

	origArgs := slices.Clone(cmd.Args)
	if len(origArgs) == 0 {
		origArgs = append(origArgs, r.Name())
	} else {
		origArgs[0] = r.Name()
	}
	fallback := exec.CommandContext(ctx, r.Name())
	fallback.Args = origArgs
	fallback.Path = origArgs[0]
	fallback.Env = slices.Clone(cmd.Env)
	fallback.Dir = cmd.Dir
	fallback.Stdin = cmd.Stdin
	fallback.Stdout = cmd.Stdout
	fallback.Stderr = cmd.Stderr
	if cmd.ExtraFiles != nil {
		fallback.ExtraFiles = slices.Clone(cmd.ExtraFiles)
	}
	fallback.SysProcAttr = cmd.SysProcAttr
	fallback.WaitDelay = cmd.WaitDelay
	return runOnce(fallback)
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

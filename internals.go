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

	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"golang.org/x/sys/unix"
)

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type runnable struct {
	payload       []byte
	file          *os.File
	closer        io.Closer
	name          string
	sha256hex     string
	deleteOnClose bool
	runner        commandrunner.Runner
}

func (r *runnable) IsMemfd() bool {
	return strings.HasPrefix(r.name, "/proc/self/fd/")
}

// switchToTemporaryFile attempts to transition the runnable from an
// in-memory file descriptor to a temporary file. It checks if the
// current setup is valid, handles errors during the process, and
// ensures proper permissions are set for the newly created temporary
// file. If the in-memory file descriptor is not valid or if the
// payload is empty, appropriate errors are returned.
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

// Name returns the name of the runnable, either from the internal
// name or the associated file's name if the internal name is empty.
func (r *runnable) Name() string {
	if r.name == "" && r.file != nil {
		return r.file.Name()
	}
	return r.name
}

// Close releases resources associated with the runnable, closing the
// file if open and removing the temporary file if it was created
// during the process.
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

// run executes the command with the provided context, handling
// fallback to a temporary file if permission errors are encountered
// with the in-memory file descriptor.
func (r *runnable) run(ctx context.Context, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	if r.runner == nil {
		r.runner = commandrunner.Default
	}
	b, err := r.runner.Run(cmd, combinedOutput)
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
	return r.runner.Run(fallback, combinedOutput)
}

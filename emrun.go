//go:build linux || android

package emrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Open creates a memory file descriptor using memfd_create(2), name
// will be a sha256 hash of the payload that will show up under
// /proc/<pid>/{fd,fdinfo}, running process will show up as
// /proc/self/fd/<int>. Returns an open file descriptor (or error)
// where Name() can be used to execute it. Close the fd when
// done. Payload can be anything Linux/Android can execute (ELF and
// script shebang). Example:
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
func Open(executablePayload []byte) (*os.File, error) {
	name := sha256hex(executablePayload)
	fd, err := unix.MemfdCreate(name, 0)
	if err != nil {
		return nil, fmt.Errorf("unable to create anonymous file: %w", err)
	}
	f := os.NewFile(uintptr(fd), fmt.Sprintf("/proc/self/fd/%d", fd))
	if _, err := f.Write(executablePayload); err != nil {
		return nil, fmt.Errorf("unable to write payload to memory: %w", err)
	}
	return f, nil
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

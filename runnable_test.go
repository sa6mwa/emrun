//go:build linux || android
// +build linux android

package emrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sa6mwa/emrun/adapters/mockrunner"
)

func TestRunnableRunFallsBackToTempfile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	payload := []byte("#!/bin/sh\necho fallback\n")
	f, err := Open(payload)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	r := f.(*runnable)
	defer r.Close()
	if !r.IsMemfd() {
		t.Skip("memfd unavailable; cannot exercise fallback path")
	}
	memfdName := r.Name()

	mock := mockrunner.New(
		func(cmd *exec.Cmd) error {
			return &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: unix.EACCES}
		},
		func(cmd *exec.Cmd) error {
			if cmd.Path == memfdName {
				t.Fatal("fallback executed memfd path")
			}
			if cmd.Stdout == nil || cmd.Stderr == nil {
				t.Fatal("expected stdout/stderr to be configured")
			}
			if _, err := cmd.Stdout.Write([]byte("fallback\n")); err != nil {
				t.Fatalf("unable to write fallback output: %v", err)
			}
			return nil
		},
	)
	r.runner = mock
	cmd := exec.CommandContext(ctx, memfdName)
	out, runErr := r.Run(ctx, cmd, true)
	if runErr != nil {
		t.Fatalf("run returned error: %v", runErr)
	}
	if string(out) != "fallback\n" {
		t.Fatalf("unexpected fallback output: %q", out)
	}
	if mock.Calls != 2 {
		t.Fatalf("unexpected number of command invocations: got %d want 2", mock.Calls)
	}
	if len(mock.Paths) != 2 {
		t.Fatalf("unexpected path count: %v", mock.Paths)
	}
	if mock.Paths[0] != memfdName {
		t.Fatalf("first execution path mismatch: got %q want %q", mock.Paths[0], memfdName)
	}
	if r.IsMemfd() {
		t.Fatalf("runnable still reports memfd after fallback: name=%q", r.Name())
	}
	if strings.HasPrefix(mock.Paths[1], "/proc/self/fd/") {
		t.Fatalf("fallback path still points at memfd: %q", mock.Paths[1])
	}
	if mock.Paths[1] != r.Name() {
		t.Fatalf("fallback command did not use tempfile: got %q want %q", mock.Paths[1], r.Name())
	}
}

func TestRunnableRunFallbackSwitchFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := &runnable{
		name:    "/proc/self/fd/999",
		payload: nil,
	}

	r.runner = mockrunner.New(func(cmd *exec.Cmd) error {
		return &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: unix.EACCES}
	})

	cmd := exec.CommandContext(ctx, r.Name())
	if _, err := r.Run(ctx, cmd, true); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !errors.Is(err, ERR_PAYLOAD_IS_EMPTY) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSwitchToTemporaryFileSuccess(t *testing.T) {
	r := &runnable{
		name:    "/proc/self/fd/123",
		payload: []byte("#!/bin/sh\necho ok\n"),
	}
	if err := r.switchToTemporaryFile(); err != nil {
		t.Fatalf("switchToTemporaryFile returned error: %v", err)
	}
	if r.IsMemfd() {
		t.Fatalf("expected runnable to no longer identify as memfd")
	}
	if !r.deleteOnClose {
		t.Fatalf("expected deleteOnClose to be true")
	}
	if r.name == "" {
		t.Fatalf("temporary file name is empty")
	}
	info, err := os.Stat(r.name)
	if err != nil {
		t.Fatalf("stat temporary file: %v", err)
	}
	if info.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("temporary file not executable: %v", info.Mode())
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Fatalf("close runnable: %v", err)
		}
	})
}

func TestSwitchToTemporaryFileErrors(t *testing.T) {
	r := &runnable{payload: []byte("data")}
	if err := r.switchToTemporaryFile(); !errors.Is(err, ERR_NOT_AN_INMEMORY_FD) {
		t.Fatalf("expected ERR_NOT_AN_INMEMORY_FD, got %v", err)
	}

	r = &runnable{name: "/proc/self/fd/123"}
	if err := r.switchToTemporaryFile(); !errors.Is(err, ERR_PAYLOAD_IS_EMPTY) {
		t.Fatalf("expected ERR_PAYLOAD_IS_EMPTY, got %v", err)
	}
}

func TestCloneCommandForFallbackClonesFields(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "/proc/self/fd/10", "arg1", "arg2")
	cmd.Env = []string{"A=B"}
	cmd.Dir = "/tmp"
	stdin := strings.NewReader("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.ExtraFiles = []*os.File{}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.WaitDelay = 123 * time.Millisecond

	fallbackPath := "/tmp/fallback"
	cloned := cloneCommandForFallback(ctx, cmd, fallbackPath)

	if cloned == cmd {
		t.Fatalf("expected new command instance")
	}
	if cloned.Path != fallbackPath {
		t.Fatalf("unexpected path: got %q want %q", cloned.Path, fallbackPath)
	}
	if len(cloned.Args) != len(cmd.Args) {
		t.Fatalf("unexpected args length: got %d want %d", len(cloned.Args), len(cmd.Args))
	}
	if cloned.Args[0] != fallbackPath {
		t.Fatalf("expected argv[0] replaced: got %q want %q", cloned.Args[0], fallbackPath)
	}
	if cloned.Args[1] != cmd.Args[1] {
		t.Fatalf("unexpected arg[1]: got %q want %q", cloned.Args[1], cmd.Args[1])
	}
	if cloned.Stdout != stdout || cloned.Stderr != stderr {
		t.Fatalf("expected stdout/stderr preserved")
	}
	if cloned.Stdin != stdin {
		t.Fatalf("expected stdin preserved")
	}
	if cloned.Dir != cmd.Dir {
		t.Fatalf("expected dir preserved")
	}
	if cloned.SysProcAttr == nil || cloned.SysProcAttr.Setsid != true {
		t.Fatalf("expected SysProcAttr cloned")
	}
	if cloned.WaitDelay != cmd.WaitDelay {
		t.Fatalf("expected WaitDelay preserved")
	}
}

func TestIsPermissionErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"os-err-permission", os.ErrPermission, true},
		{"unix-eacces", unix.EACCES, true},
		{"path-error", &os.PathError{Err: unix.EPERM}, true},
		{"exec-error", &exec.Error{Err: unix.EACCES}, true},
		{"other", errors.New("boom"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPermissionErr(tc.err)
			if got != tc.want {
				t.Fatalf("isPermissionErr(%v) = %v want %v", tc.err, got, tc.want)
			}
		})
	}
}

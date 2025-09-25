package emrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sa6mwa/emrun/adapters/mockrunner"
)

func TestSHA256Hex(t *testing.T) {
	got := sha256hex([]byte("test"))
	const want = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got != want {
		t.Fatalf("sha256hex mismatch: got %q want %q", got, want)
	}
}

func TestOpenCreatesExecutableMemfd(t *testing.T) {
	payload := []byte("#!/bin/sh\necho open-test\n")
	f, err := Open(payload)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { f.Close() })

	if !strings.HasPrefix(f.Name(), "/proc/self/fd/") {
		t.Fatalf("unexpected file name %q", f.Name())
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("failed to seek memfd: %v", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read memfd: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("memfd contents mismatch: got %q want %q", data, payload)
	}
}

func TestRunExecutesPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := []byte("#!/bin/sh\nprintf 'arg:%s\\n' \"$1\"\n")
	out, err := Run(ctx, payload, "value")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	const want = "arg:value\n"
	if string(out) != want {
		t.Fatalf("Run output mismatch: got %q want %q", out, want)
	}
}

func TestDoExecutesPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := "#!/bin/sh\nprintf 'do:%s\\n' \"$1\"\n"
	out, err := Do(ctx, payload, "value")
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	const want = "do:value\n"
	if string(out) != want {
		t.Fatalf("Do output mismatch: got %q want %q", out, want)
	}
}

func TestRunIORoutesIO(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := []byte("#!/bin/sh\nread line\nprintf 'stdout:%s\\n' \"$line\"\nprintf 'stderr:%s\\n' \"$line\" 1>&2\n")
	input := "hello\n"

	var buf bytes.Buffer
	if err := RunIO(ctx, strings.NewReader(input), &buf, payload); err != nil {
		t.Fatalf("RunIO returned error: %v", err)
	}

	const want = "stdout:hello\nstderr:hello\n"
	if buf.String() != want {
		t.Fatalf("RunIO combined output mismatch: got %q want %q", buf.String(), want)
	}
}

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
		func(cmd *exec.Cmd, _ bool) ([]byte, error) {
			return nil, &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: unix.EACCES}
		},
		func(cmd *exec.Cmd, _ bool) ([]byte, error) {
			if cmd.Path == memfdName {
				t.Fatal("fallback executed memfd path")
			}
			return []byte("fallback\n"), nil
		},
	)
	r.runner = mock
	cmd := exec.CommandContext(ctx, memfdName)
	out, runErr := r.run(ctx, cmd, true)
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

	r.runner = mockrunner.New(func(cmd *exec.Cmd, _ bool) ([]byte, error) {
		return nil, &os.PathError{Op: "fork/exec", Path: cmd.Path, Err: unix.EACCES}
	})

	cmd := exec.CommandContext(ctx, r.Name())
	if _, err := r.run(ctx, cmd, true); err == nil {
		t.Fatalf("expected error, got nil")
	} else if !errors.Is(err, ERR_PAYLOAD_IS_EMPTY) {
		t.Fatalf("unexpected error: %v", err)
	}
}

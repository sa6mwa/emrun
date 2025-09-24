package emrun

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
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

package efrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesExecutableTempfile(t *testing.T) {
	payload := []byte("#!/bin/sh\necho open-test\n")
	f, err := Open(payload)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	runnable := f.(*runnable)

	if runnable.IsMemfd() {
		t.Fatalf("expected IsMemfd to be false")
	}

	name := runnable.Name()
	if name == "" {
		t.Fatalf("runnable name is empty")
	}

	info, err := os.Stat(name)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("temporary file is not executable: mode=%v", info.Mode())
	}

	if _, err := runnable.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek failed: %v", err)
	}
	data, err := io.ReadAll(runnable)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch: got %q want %q", data, payload)
	}

	if err := runnable.Close(); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file still exists after close: err=%v", err)
	}
}

func TestOpenRejectsEmptyPayload(t *testing.T) {
	if _, err := Open(nil); !errors.Is(err, ERR_PAYLOAD_IS_EMPTY) {
		t.Fatalf("expected ERR_PAYLOAD_IS_EMPTY, got %v", err)
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

	var buf bytes.Buffer
	if err := RunIO(ctx, strings.NewReader("hello\n"), &buf, payload); err != nil {
		t.Fatalf("RunIO returned error: %v", err)
	}

	const want = "stdout:hello\nstderr:hello\n"
	if buf.String() != want {
		t.Fatalf("RunIO combined output mismatch: got %q want %q", buf.String(), want)
	}
}

func TestRunIOERoutesSeparateWriters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := []byte("#!/bin/sh\nread line\nprintf 'out:%s\\n' \"$line\"\nprintf 'err:%s\\n' \"$line\" 1>&2\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := RunIOE(ctx, strings.NewReader("value\n"), &stdout, &stderr, payload); err != nil {
		t.Fatalf("RunIOE returned error: %v", err)
	}

	if stdout.String() != "out:value\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.String() != "err:value\n" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

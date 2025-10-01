//go:build linux || android
// +build linux android

package emrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

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

func TestRunIOERoutesSeparateWriters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := []byte("#!/bin/sh\nread line\nprintf 'out:%s\\n' \"$line\"\nprintf 'err:%s\\n' \"$line\" 1>&2\n")

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	if err := RunIOE(ctx, strings.NewReader("value\n"), &stdoutBuf, &stderrBuf, payload); err != nil {
		t.Fatalf("RunIOE returned error: %v", err)
	}

	if stdoutBuf.String() != "out:value\n" {
		t.Fatalf("unexpected stdout: %q", stdoutBuf.String())
	}
	if stderrBuf.String() != "err:value\n" {
		t.Fatalf("unexpected stderr: %q", stderrBuf.String())
	}
}

func TestRunBGReturnsBackgroundResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte("#!/bin/sh\nprintf 'bg:%s\\n' \"$1\"\n")
	bg, err := RunBG(ctx, payload, "value")
	if err != nil {
		t.Fatalf("RunBG returned error: %v", err)
	}
	res := bg.Wait()
	if res.Error != nil {
		t.Fatalf("background run failed: %v", res.Error)
	}
	if string(res.CombinedOutput) != "bg:value\n" {
		t.Fatalf("unexpected combined output: %q", res.CombinedOutput)
	}
}

func TestRunIOBGStreamsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte("#!/bin/sh\nread line\nprintf 'io:%s\\n' \"$line\"\nprintf 'err:%s\\n' \"$line\" 1>&2\n")
	var out bytes.Buffer
	bg, err := RunIOBG(ctx, strings.NewReader("demo\n"), &out, payload)
	if err != nil {
		t.Fatalf("RunIOBG returned error: %v", err)
	}
	res := bg.Wait()
	if res.Error != nil {
		t.Fatalf("background run failed: %v", res.Error)
	}
	if res.CombinedOutput != nil {
		t.Fatalf("expected nil combined output, got %q", res.CombinedOutput)
	}
	if out.String() != "io:demo\nerr:demo\n" {
		t.Fatalf("unexpected streamed output: %q", out.String())
	}
}

func TestRunIOEBGSeparatesStreams(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte("#!/bin/sh\nread line\nprintf 'stdout:%s\\n' \"$line\"\nprintf 'stderr:%s\\n' \"$line\" 1>&2\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	bg, err := RunIOEBG(ctx, strings.NewReader("demo\n"), &stdout, &stderr, payload)
	if err != nil {
		t.Fatalf("RunIOEBG returned error: %v", err)
	}
	res := bg.Wait()
	if res.Error != nil {
		t.Fatalf("background run failed: %v", res.Error)
	}
	if res.CombinedOutput != nil {
		t.Fatalf("expected nil combined output, got %q", res.CombinedOutput)
	}
	if stdout.String() != "stdout:demo\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.String() != "stderr:demo\n" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestDoBGMatchesDo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bg, err := DoBG(ctx, "#!/bin/sh\nprintf 'from:%s\\n' \"$1\"\n", "here")
	if err != nil {
		t.Fatalf("DoBG returned error: %v", err)
	}
	res := bg.Wait()
	if res.Error != nil {
		t.Fatalf("background run failed: %v", res.Error)
	}
	if string(res.CombinedOutput) != "from:here\n" {
		t.Fatalf("unexpected combined output: %q", res.CombinedOutput)
	}
}

func TestRunDeniedByPolicy(t *testing.T) {
	ctx := WithPolicy(context.Background(), DENY)
	payload := []byte("#!/bin/sh\necho blocked\n")
	_, err := Run(ctx, payload)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
}

func TestRunAllowedByPolicy(t *testing.T) {
	payload := []byte("#!/bin/sh\necho allowed\n")
	sum := sha256.Sum256(payload)
	hexDigest := hex.EncodeToString(sum[:])
	ctx := WithPolicy(context.Background(), DENY)
	ctx = WithRule(ctx, ALLOW, hexDigest)
	if _, err := Run(ctx, payload); err != nil {
		t.Fatalf("Run returned error under allow policy: %v", err)
	}
}

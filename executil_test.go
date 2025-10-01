package emrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/sa6mwa/emrun/adapters/commandrunner"
	"github.com/sa6mwa/emrun/adapters/mockrunner"
)

func TestRunCommandCombinedOutput(t *testing.T) {
	runner := mockrunner.New(func(cmd *exec.Cmd) error {
		if _, err := cmd.Stdout.Write([]byte("stdout\n")); err != nil {
			t.Fatalf("write stdout: %v", err)
		}
		if _, err := cmd.Stderr.Write([]byte("stderr\n")); err != nil {
			t.Fatalf("write stderr: %v", err)
		}
		return nil
	})

	cmd := exec.Command("/bin/true")
	out, err := RunCommand(runner, cmd, true)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	got := string(out)
	if got != "stdout\nstderr\n" {
		t.Fatalf("unexpected combined output: %q", got)
	}
}

func TestRunCommandPassThroughWriters(t *testing.T) {
	buf := &bytes.Buffer{}
	runner := mockrunner.New(func(cmd *exec.Cmd) error {
		if _, err := cmd.Stdout.Write([]byte("hello")); err != nil {
			t.Fatalf("write stdout: %v", err)
		}
		return nil
	})
	cmd := exec.Command("/bin/true")
	cmd.Stdout = buf
	if _, err := RunCommand(runner, cmd, false); err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if buf.String() != "hello" {
		t.Fatalf("unexpected stdout: %q", buf.String())
	}
}

func TestRunCommandNilRunner(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if _, err := RunCommand(nil, cmd, true); err == nil {
		t.Fatalf("expected error for nil runner")
	}
}

func TestStartCommandCombinedOutput(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "echo stdout; echo stderr 1>&2")
	capture, err := StartCommand(commandrunner.Default, cmd, true)
	if err != nil {
		t.Fatalf("StartCommand failed: %v", err)
	}
	res := WaitCommand(cmd, capture)
	if res.Error != nil {
		t.Fatalf("WaitCommand returned error: %v", res.Error)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", res.ExitCode)
	}
	if string(res.CombinedOutput) != "stdout\nstderr\n" {
		t.Fatalf("unexpected combined output: %q", res.CombinedOutput)
	}
}

func TestStartCommandCombinedOutputConfiguredWriters(t *testing.T) {
	cmd := exec.Command("/bin/true")
	cmd.Stdout = io.Discard
	if _, err := StartCommand(commandrunner.Default, cmd, true); err == nil {
		t.Fatalf("expected error when stdout already configured")
	}
}

func TestStartCommandNilRunner(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if _, err := StartCommand(nil, cmd, false); err == nil {
		t.Fatalf("expected error for nil runner")
	}
}

func TestExitCodeFrom(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit error")
	}
	if code := exitCodeFrom(err, nil); code != 7 {
		t.Fatalf("unexpected exit code from error: %d", code)
	}

	cmd2 := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd2.Run(); err != nil {
		t.Fatalf("unexpected error running cmd2: %v", err)
	}
	if code := exitCodeFrom(nil, cmd2.ProcessState); code != 0 {
		t.Fatalf("unexpected exit code from process state: %d", code)
	}

	if code := exitCodeFrom(errors.New("boom"), nil); code != -1 {
		t.Fatalf("expected -1 for unknown error, got %d", code)
	}
}

func TestStartBackground(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte("#!/bin/sh\necho bg\n")
	r, err := Open(payload)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	bg, err := StartBackground(ctx, r.(*runnable), nil, nil, nil, nil, true)
	if err != nil {
		t.Fatalf("StartBackground failed: %v", err)
	}
	res := bg.Wait()
	if res.Error != nil {
		t.Fatalf("background finished with error: %v", res.Error)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", res.ExitCode)
	}
	if string(res.CombinedOutput) != "bg\n" {
		t.Fatalf("unexpected combined output: %q", res.CombinedOutput)
	}
}

func TestStartBackgroundCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	payload := []byte("#!/bin/sh\nsleep 2\n")
	r, err := Open(payload)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	bg, err := StartBackground(ctx, r.(*runnable), nil, nil, nil, nil, true)
	if err != nil {
		t.Fatalf("StartBackground failed: %v", err)
	}
	cancel()
	res := bg.Wait()
	if res.Error == nil {
		t.Fatalf("expected cancellation error")
	}
}

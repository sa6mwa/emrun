package commandrunner_test

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/sa6mwa/emrun/adapters/commandrunner"
)

func TestDefaultRunnerCombinedOutput(t *testing.T) {
	runner := commandrunner.DefaultRunner{}
	cmd := exec.Command("/bin/sh", "-c", "echo combined && echo err >&2")

	out, err := runner.Run(cmd, true)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got := string(out)
	if got != "combined\nerr\n" {
		t.Fatalf("unexpected combined output: %q", got)
	}
}

func TestDefaultRunnerStreamedOutput(t *testing.T) {
	runner := commandrunner.DefaultRunner{}
	cmd := exec.Command("/bin/sh", "-c", "echo streamed")
	var buf bytes.Buffer
	cmd.Stdout = &buf

	out, err := runner.Run(cmd, false)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil combined output, got %q", out)
	}
	if buf.String() != "streamed\n" {
		t.Fatalf("unexpected streamed output: %q", buf.String())
	}
}

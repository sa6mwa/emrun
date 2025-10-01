package commandrunner_test

import (
	"os/exec"
	"testing"

	"github.com/sa6mwa/emrun/adapters/commandrunner"
)

func TestDefaultRunnerRun(t *testing.T) {
	runner := commandrunner.DefaultRunner{}
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := runner.Run(cmd); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestDefaultRunnerStart(t *testing.T) {
	runner := commandrunner.DefaultRunner{}
	cmd := exec.Command("/bin/sh", "-c", "sleep 0.1")
	if err := runner.Start(cmd); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
}

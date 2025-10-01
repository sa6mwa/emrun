package mockrunner

import (
	"errors"
	"testing"

	"os/exec"
)

func TestRunnerRunRecordsCallMetadata(t *testing.T) {
	runner := New(func(cmd *exec.Cmd) error {
		if cmd.Path != "first-path" {
			t.Fatalf("unexpected command path: %q", cmd.Path)
		}
		return nil
	})

	cmd := &exec.Cmd{Path: "first-path"}

	if err := runner.Run(cmd); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if runner.Calls != 1 {
		t.Fatalf("Calls = %d, want 1", runner.Calls)
	}
	if len(runner.Paths) != 1 || runner.Paths[0] != "first-path" {
		t.Fatalf("Paths recorded %v, want [first-path]", runner.Paths)
	}
	if remaining := runner.Remaining(); remaining != 0 {
		t.Fatalf("Remaining() = %d, want 0", remaining)
	}
}

func TestRunnerRunSequentialBehaviors(t *testing.T) {
	sentinel := errors.New("sentinel")
	runner := New(
		func(cmd *exec.Cmd) error {
			if cmd.Path != "first" {
				t.Fatalf("first behavior got path %q", cmd.Path)
			}
			return nil
		},
		func(cmd *exec.Cmd) error {
			if cmd.Path != "second" {
				t.Fatalf("second behavior got path %q", cmd.Path)
			}
			return sentinel
		},
	)

	if err := runner.Run(&exec.Cmd{Path: "first"}); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}

	if err := runner.Run(&exec.Cmd{Path: "second"}); !errors.Is(err, sentinel) {
		t.Fatalf("second Run error = %v, want sentinel", err)
	}

	if err := runner.Run(&exec.Cmd{Path: "third"}); err != nil {
		t.Fatalf("third Run returned error: %v", err)
	}

	if runner.Calls != 3 {
		t.Fatalf("Calls = %d, want 3", runner.Calls)
	}

	wantPaths := []string{"first", "second", "third"}
	if len(runner.Paths) != len(wantPaths) {
		t.Fatalf("Paths length = %d, want %d", len(runner.Paths), len(wantPaths))
	}
	for i, want := range wantPaths {
		if got := runner.Paths[i]; got != want {
			t.Fatalf("Paths[%d] = %q, want %q", i, got, want)
		}
	}

	if remaining := runner.Remaining(); remaining != 0 {
		t.Fatalf("Remaining() = %d, want 0", remaining)
	}
}

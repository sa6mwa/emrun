package mockrunner

import (
	"errors"
	"testing"

	"os/exec"
)

func TestRunnerRunRecordsCallMetadata(t *testing.T) {
	runner := New(func(cmd *exec.Cmd, combined bool) ([]byte, error) {
		if cmd.Path != "first-path" {
			t.Fatalf("unexpected command path: %q", cmd.Path)
		}
		if !combined {
			t.Fatalf("unexpected combined flag: got %v, want true", combined)
		}
		return []byte("payload"), nil
	})

	cmd := &exec.Cmd{Path: "first-path"}

	out, err := runner.Run(cmd, true)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if string(out) != "payload" {
		t.Fatalf("Run returned %q, want %q", string(out), "payload")
	}

	if runner.Calls != 1 {
		t.Fatalf("Calls = %d, want 1", runner.Calls)
	}
	if len(runner.Paths) != 1 || runner.Paths[0] != "first-path" {
		t.Fatalf("Paths recorded %v, want [first-path]", runner.Paths)
	}
	if len(runner.Combined) != 1 || !runner.Combined[0] {
		t.Fatalf("Combined recorded %v, want [true]", runner.Combined)
	}
	if remaining := runner.Remaining(); remaining != 0 {
		t.Fatalf("Remaining() = %d, want 0", remaining)
	}
}

func TestRunnerRunSequentialBehaviors(t *testing.T) {
	sentinel := errors.New("sentinel")
	runner := New(
		func(cmd *exec.Cmd, combined bool) ([]byte, error) {
			if cmd.Path != "first" {
				t.Fatalf("first behavior got path %q", cmd.Path)
			}
			if combined {
				t.Fatalf("first behavior expected combined=false")
			}
			return []byte("first-output"), nil
		},
		func(cmd *exec.Cmd, combined bool) ([]byte, error) {
			if cmd.Path != "second" {
				t.Fatalf("second behavior got path %q", cmd.Path)
			}
			if !combined {
				t.Fatalf("second behavior expected combined=true")
			}
			return nil, sentinel
		},
	)

	first, err := runner.Run(&exec.Cmd{Path: "first"}, false)
	if err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	if string(first) != "first-output" {
		t.Fatalf("first Run returned %q, want %q", string(first), "first-output")
	}

	second, err := runner.Run(&exec.Cmd{Path: "second"}, true)
	if !errors.Is(err, sentinel) {
		t.Fatalf("second Run error = %v, want sentinel", err)
	}
	if second != nil {
		t.Fatalf("second Run returned non-nil output: %q", string(second))
	}

	third, err := runner.Run(&exec.Cmd{Path: "third"}, false)
	if err != nil {
		t.Fatalf("third Run returned error: %v", err)
	}
	if third != nil {
		t.Fatalf("third Run returned output %q, want nil", string(third))
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

	wantCombined := []bool{false, true, false}
	if len(runner.Combined) != len(wantCombined) {
		t.Fatalf("Combined length = %d, want %d", len(runner.Combined), len(wantCombined))
	}
	for i, want := range wantCombined {
		if got := runner.Combined[i]; got != want {
			t.Fatalf("Combined[%d] = %v, want %v", i, got, want)
		}
	}

	if remaining := runner.Remaining(); remaining != 0 {
		t.Fatalf("Remaining() = %d, want 0", remaining)
	}
}

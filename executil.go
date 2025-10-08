package emrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"pkt.systems/emrun/adapters/commandcapture"
	"pkt.systems/emrun/port"
)

// RunCommand executes cmd using the supplied runner. When combinedOutput is
// true the function captures stdout and stderr into a shared buffer and returns
// it as a copy to the caller. Otherwise RunCommand defers to the runner without
// altering the configured streams.
func RunCommand(runner port.CommandRunner, cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	if runner == nil {
		return nil, fmt.Errorf("nil command runner")
	}
	capture, err := newCommandCapture(cmd, combinedOutput)
	if err != nil {
		return nil, err
	}
	err = runner.Run(cmd)
	return capture.Finish(), err
}

// StartCommand starts cmd using the supplied runner while optionally capturing
// combined stdout/stderr. The returned CommandCapture must later be passed to
// WaitCommand (or Restore via Finish) to release resources.
func StartCommand(runner port.CommandRunner, cmd *exec.Cmd, combinedOutput bool) (port.CommandCapture, error) {
	if runner == nil {
		return nil, fmt.Errorf("nil command runner")
	}
	capture, err := newCommandCapture(cmd, combinedOutput)
	if err != nil {
		return nil, err
	}
	if err := runner.Start(cmd); err != nil {
		capture.Restore()
		return nil, err
	}
	return capture, nil
}

// WaitCommand waits for cmd to exit and returns a Result capturing the exit
// code, error, and any combined output buffered by StartCommand.
func WaitCommand(cmd *exec.Cmd, capture port.CommandCapture) Result {
	var res Result
	err := cmd.Wait()
	res.Error = err
	res.ExitCode = exitCodeFrom(err, cmd.ProcessState)
	if capture != nil {
		res.CombinedOutput = capture.Finish()
	}
	return res
}

func newCommandCapture(cmd *exec.Cmd, combined bool) (port.CommandCapture, error) {
	capture := commandcapture.New()
	if !combined {
		return capture, nil
	}
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	if cmd.Stdout != nil || cmd.Stderr != nil {
		return nil, fmt.Errorf("combined output requested with configured stdout or stderr")
	}
	buf := &bytes.Buffer{}
	buf.Grow(128)
	origStdout, origStderr := cmd.Stdout, cmd.Stderr
	cmd.Stdout = buf
	cmd.Stderr = buf
	capture.Enable(buf, func() {
		cmd.Stdout = origStdout
		cmd.Stderr = origStderr
	})
	return capture, nil
}

func exitCodeFrom(waitErr error, state *os.ProcessState) int {
	if state != nil {
		return state.ExitCode()
	}
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ProcessState != nil {
		return exitErr.ProcessState.ExitCode()
	}
	return -1
}

// StartBackground launches cmd via the runnable, wiring optional stdio streams
// and returning a Background handle that reports completion through Done.
func StartBackground(parentCtx context.Context, run port.BackgroundRunnable, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, combined bool) (*Background, error) {
	ctx, cancel := context.WithCancel(parentCtx)
	cmd := exec.CommandContext(ctx, run.Name(), args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	startedCmd, capture, err := run.StartBackground(ctx, cmd, combined)
	if err != nil {
		run.Close()
		cancel()
		return nil, err
	}
	done := make(chan Result, 1)
	var once sync.Once
	go func(rn port.BackgroundRunnable, cap port.CommandCapture, execCmd *exec.Cmd, closer context.CancelFunc) {
		res := WaitCommand(execCmd, cap)
		if err := rn.Close(); err != nil && res.Error == nil {
			res.Error = err
		}
		once.Do(func() {
			done <- res
			close(done)
		})
		closer()
	}(run, capture, startedCmd, cancel)
	return &Background{
		Context: ctx,
		Cancel:  cancel,
		Done:    done,
	}, nil
}

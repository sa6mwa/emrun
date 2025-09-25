package commandrunner

import "os/exec"

// Runner abstracts command execution so tests can provide mocks while production
// code can delegate to os/exec.
type Runner interface {
	Run(cmd *exec.Cmd, combinedOutput bool) ([]byte, error)
}

// DefaultRunner executes commands using os/exec directly.
type DefaultRunner struct{}

// Run executes the command, optionally collecting combined stdout/stderr.
func (DefaultRunner) Run(cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	if combinedOutput {
		return cmd.CombinedOutput()
	}
	return nil, cmd.Run()
}

// Default is a shared instance of DefaultRunner.
var Default Runner = DefaultRunner{}

package commandrunner

import (
	"os/exec"

	"github.com/sa6mwa/emrun/port"
)

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
var Default port.CommandRunner = DefaultRunner{}

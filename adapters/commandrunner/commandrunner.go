package commandrunner

import (
	"os/exec"

	"pkt.systems/emrun/port"
)

// DefaultRunner executes commands using os/exec directly.
type DefaultRunner struct{}

// Run executes the command using cmd.Run().
func (DefaultRunner) Run(cmd *exec.Cmd) error {
	return cmd.Run()
}

// Start begins executing the command using cmd.Start().
func (DefaultRunner) Start(cmd *exec.Cmd) error {
	return cmd.Start()
}

// Default is a shared instance of DefaultRunner.
var Default port.CommandRunner = DefaultRunner{}

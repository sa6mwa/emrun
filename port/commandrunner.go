package port

import (
	"os/exec"
)

// CommandRunner abstracts command execution so runners can be plugged in across
// packages without depending on a specific adapter implementation.
type CommandRunner interface {
	Run(cmd *exec.Cmd) error
	Start(cmd *exec.Cmd) error
}

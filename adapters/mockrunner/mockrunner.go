package mockrunner

import (
	"os/exec"
	"slices"
	"sync"

	"github.com/sa6mwa/emrun/port"
)

// Behavior represents a single command execution path for the mock runner.
type Behavior func(cmd *exec.Cmd, combinedOutput bool) ([]byte, error)

// Runner is a thread-safe mock implementation of the commandrunner.Runner interface.
type Runner struct {
	mu        sync.Mutex
	behaviors []Behavior
	Calls     int
	Paths     []string
	Combined  []bool
}

var _ port.CommandRunner = (*Runner)(nil)

// New constructs a Runner that will invoke behaviors sequentially for each call.
func New(behaviors ...Behavior) *Runner {
	return &Runner{behaviors: slices.Clone(behaviors)}
}

// Run records the call metadata and dispatches to the next behavior.
func (r *Runner) Run(cmd *exec.Cmd, combinedOutput bool) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.Calls++
	r.Paths = append(r.Paths, cmd.Path)
	r.Combined = append(r.Combined, combinedOutput)

	if len(r.behaviors) == 0 {
		return nil, nil
	}
	behavior := r.behaviors[0]
	r.behaviors = r.behaviors[1:]
	return behavior(cmd, combinedOutput)
}

// Remaining returns the number of queued behaviors that have not yet been consumed.
func (r *Runner) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.behaviors)
}

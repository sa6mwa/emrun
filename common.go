package emrun

import "context"

type Background struct {
	Context context.Context
	Cancel  context.CancelFunc
	Done    <-chan Result
}

// Wait blocks until the background command finishes or the stored context is
// cancelled. It returns the underlying Result; if the stored context is nil it
// behaves like WaitWithContext(context.Background()).
func (bg *Background) Wait() Result {
	if bg == nil {
		return Result{}
	}
	ctx := bg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return bg.WaitWithContext(ctx)
}

// WaitWithContext blocks until the background command completes or ctx is
// cancelled. Cancellation returns a Result whose Error is ctx.Err().
func (bg *Background) WaitWithContext(ctx context.Context) Result {
	if bg == nil {
		return Result{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if bg.Done == nil {
		return Result{}
	}
	select {
	case res, ok := <-bg.Done:
		if !ok {
			return Result{}
		}
		return res
	case <-ctx.Done():
		return Result{Error: ctx.Err()}
	}
}

type Result struct {
	ExitCode       int
	Error          error
	CombinedOutput []byte
}

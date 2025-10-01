package emrun

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackgroundWaitReturnsResult(t *testing.T) {
	done := make(chan Result, 1)
	want := Result{ExitCode: 42}
	done <- want
	bg := &Background{Done: done}
	got := bg.Wait()
	if got.ExitCode != want.ExitCode {
		t.Fatalf("unexpected exit code: got %d want %d", got.ExitCode, want.ExitCode)
	}
}

func TestBackgroundWaitNilReceiver(t *testing.T) {
	var bg *Background
	got := bg.Wait()
	if got.ExitCode != 0 || got.Error != nil || len(got.CombinedOutput) != 0 {
		t.Fatalf("expected zero result, got %#v", got)
	}
}

func TestBackgroundWaitRespectsStoredContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bg := &Background{Context: ctx, Done: make(chan Result)}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	res := bg.Wait()
	if !errors.Is(res.Error, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", res.Error)
	}
}

func TestBackgroundWaitWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bg := &Background{Done: make(chan Result)}
	cancel()
	res := bg.WaitWithContext(ctx)
	if !errors.Is(res.Error, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", res.Error)
	}
}

func TestBackgroundWaitWithContextNilBackground(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var bg *Background
	res := bg.WaitWithContext(ctx)
	if res.ExitCode != 0 || res.Error != nil || len(res.CombinedOutput) != 0 {
		t.Fatalf("expected zero result, got %#v", res)
	}
}

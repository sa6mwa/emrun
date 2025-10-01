package commandcapture

import (
	"bytes"
	"testing"
)

type stubBuffer struct {
	data []byte
}

func (s *stubBuffer) Grow(int) {}

func (s *stubBuffer) Bytes() []byte {
	return s.data
}

func TestEnableWithBytesBuffer(t *testing.T) {
	cap := New()
	buf := &bytes.Buffer{}
	var resetCalled int
	cap.Enable(buf, func() { resetCalled++ })
	buf.WriteString("hello")
	out := cap.Finish()
	if string(out) != "hello" {
		t.Fatalf("unexpected combined output: %q", out)
	}
	if resetCalled != 1 {
		t.Fatalf("expected reset to be called once, got %d", resetCalled)
	}
	if buf.String() != "hello" {
		t.Fatalf("buffer contents modified: %q", buf.String())
	}
}

func TestEnableWithStubBuffer(t *testing.T) {
	cap := New()
	fake := &stubBuffer{data: []byte("initial")}
	cap.Enable(fake, nil)
	out := cap.Finish()
	if string(out) != "initial" {
		t.Fatalf("expected cloned data, got %q", out)
	}
}

func TestRestoreIdempotent(t *testing.T) {
	cap := New()
	buf := &bytes.Buffer{}
	var resetCalled int
	cap.Enable(buf, func() { resetCalled++ })
	cap.Restore()
	cap.Restore()
	if resetCalled != 1 {
		t.Fatalf("expected reset to be called once, got %d", resetCalled)
	}
}

func TestFinishWithoutEnableReturnsNil(t *testing.T) {
	cap := New()
	if out := cap.Finish(); out != nil {
		t.Fatalf("expected nil output, got %q", out)
	}
}

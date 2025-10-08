package commandcapture

import (
	"bytes"
	"slices"
	"sync"

	"pkt.systems/emrun/port"
)

// capture implements port.CommandCapture.
type capture struct {
	buf    *bytes.Buffer
	reset  func()
	enable bool
	once   sync.Once
}

// New constructs a new port.CommandCapture implementation.
func New() port.CommandCapture {
	return &capture{}
}

func (c *capture) Enable(buf port.Buffer, reset func()) {
	b, ok := buf.(*bytes.Buffer)
	if !ok {
		bb := &bytes.Buffer{}
		bb.Grow(128)
		bb.Write(buf.Bytes())
		c.buf = bb
	} else {
		c.buf = b
	}
	c.reset = reset
	c.enable = true
}

func (c *capture) Finish() []byte {
	c.Restore()
	if !c.enable || c.buf == nil {
		return nil
	}
	return slices.Clone(c.buf.Bytes())
}

func (c *capture) Restore() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		if c.reset != nil {
			c.reset()
			c.reset = nil
		}
	})
}

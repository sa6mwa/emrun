package port

// CommandCapture captures combined stdout/stderr for commands. Implementations
// are provided by adapters/commandcapture.
type CommandCapture interface {
	Enable(buf Buffer, reset func())
	Finish() []byte
	Restore()
}

// Buffer abstracts the minimal buffer API needed by CommandCapture.
type Buffer interface {
	Grow(int)
	Bytes() []byte
}

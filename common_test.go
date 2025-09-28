package emrun

import "testing"

func TestSHA256Hex(t *testing.T) {
	got := sha256hex([]byte("test"))
	const want = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got != want {
		t.Fatalf("sha256hex mismatch: got %q want %q", got, want)
	}
}

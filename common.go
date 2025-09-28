package emrun

import (
	"crypto/sha256"
	"encoding/hex"
)

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

package token

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a fresh 256-bit session token encoded as hex (64 chars).
func New() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

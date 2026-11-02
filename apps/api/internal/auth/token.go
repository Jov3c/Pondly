package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

const tokenPrefix = "pndl_"

// NewAgentToken returns a high-entropy opaque token and its SHA-256 digest.
// Only the digest is persisted; the plaintext is returned to the runtime once.
func NewAgentToken() (string, [32]byte, error) {
	return NewOpaqueToken(tokenPrefix)
}

func NewOpaqueToken(prefix string) (string, [32]byte, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	plain := prefix + base64.RawURLEncoding.EncodeToString(raw[:])
	return plain, HashToken(plain), nil
}

func NewNumericCode(digits int) (string, error) {
	if digits < 4 || digits > 12 {
		return "", errors.New("invalid code length")
	}
	const alphabet = "0123456789"
	out := make([]byte, digits)
	buf := make([]byte, digits)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func HashToken(plain string) [32]byte {
	return sha256.Sum256([]byte(plain))
}

func ValidTokenShape(plain string) error {
	if len(plain) < 16 || len(plain) > 512 {
		return errors.New("invalid token length")
	}
	for _, r := range plain {
		if r <= ' ' || r > '~' {
			return errors.New("invalid token character")
		}
	}
	return nil
}

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"
)

func CSRFToken(sessionID uuid.UUID, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("csrf key must be 32 bytes")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(sessionID[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func VerifyCSRF(sessionID uuid.UUID, key []byte, token string) bool {
	want, err := CSRFToken(sessionID, key)
	if err != nil || token == "" {
		return false
	}
	return hmac.Equal([]byte(want), []byte(token))
}

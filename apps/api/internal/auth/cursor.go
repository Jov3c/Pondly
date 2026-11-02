package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const cursorVersion = "1"

var (
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrCursorExpired = errors.New("cursor expired")
)

func EncodeCursor(key []byte, agentID uuid.UUID, seq int64) (string, error) {
	if len(key) != 32 {
		return "", errors.New("cursor key must be 32 bytes")
	}
	if seq < 0 {
		return "", ErrInvalidCursor
	}
	payload := fmt.Sprintf("%s|%d|%s", agentID.String(), seq, cursorVersion)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func DecodeCursor(key []byte, token string) (uuid.UUID, int64, error) {
	var zero uuid.UUID
	if len(key) != 32 {
		return zero, 0, errors.New("cursor key must be 32 bytes")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return zero, 0, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, 0, ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, 0, ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return zero, 0, ErrInvalidCursor
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 3 || fields[2] != cursorVersion {
		return zero, 0, ErrInvalidCursor
	}
	agentID, err := uuid.Parse(fields[0])
	if err != nil {
		return zero, 0, ErrInvalidCursor
	}
	seq, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || seq < 0 {
		return zero, 0, ErrInvalidCursor
	}
	return agentID, seq, nil
}

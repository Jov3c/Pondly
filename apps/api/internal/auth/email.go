package auth

import (
	"fmt"
	"net/mail"
	"strings"
)

func NormalizeEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 254 {
		return "", fmt.Errorf("invalid email")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Name != "" {
		return "", fmt.Errorf("invalid email")
	}
	normalized := strings.ToLower(addr.Address)
	if !strings.Contains(normalized, "@") || strings.HasPrefix(normalized, "@") || strings.HasSuffix(normalized, "@") {
		return "", fmt.Errorf("invalid email")
	}
	return normalized, nil
}

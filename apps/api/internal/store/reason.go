package store

import (
	"strings"
	"unicode/utf8"
)

// Justification bounds frozen in contracts/openapi.json: every governance
// decision carries 10..1000 runes except hiding a community post, which is
// capped at 500.
const (
	MinReasonRunes           = 10
	MaxReasonRunes           = 1000
	MaxModerationReasonRunes = 500
)

// NormalizeReason trims a justification and enforces the bounds, so the HTTP
// layer and the writers cannot disagree about what a reason may look like.
func NormalizeReason(reason string, maxRunes int) (string, error) {
	trimmed := strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(trimmed); n < MinReasonRunes || n > maxRunes {
		return "", ErrInvalid
	}
	return trimmed, nil
}

package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeReasonAcceptsOnlyTheSharedBounds(t *testing.T) {
	cases := []struct {
		name  string
		input string
		ok    bool
	}{
		{"too short", "同意发布", false},
		{"exactly ten runes", strings.Repeat("鱼", MinReasonRunes), true},
		{"nine runes padded by spaces", " " + strings.Repeat("鱼", MinReasonRunes-1) + " ", false},
		{"at the cap", strings.Repeat("鱼", MaxReasonRunes), true},
		{"one over the cap", strings.Repeat("鱼", MaxReasonRunes+1), false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, err := NormalizeReason(tc.input, MaxReasonRunes)
			if tc.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if tc.ok && reason != strings.TrimSpace(tc.input) {
				t.Fatalf("returned reason %q is not the trimmed input", reason)
			}
		})
	}
}

func TestNormalizeReasonCountsRunesNotBytes(t *testing.T) {
	reason := strings.Repeat("养鱼", MinReasonRunes/2)
	if utf8.RuneCountInString(reason) != MinReasonRunes {
		t.Fatalf("test input must be exactly %d runes, got %d bytes", MinReasonRunes, len(reason))
	}
	if _, err := NormalizeReason(reason, MaxReasonRunes); err != nil {
		t.Fatalf("multibyte reason rejected: %v", err)
	}
}

func TestNormalizeReasonHonoursPerCallCap(t *testing.T) {
	reason := strings.Repeat("鱼", 600)
	if _, err := NormalizeReason(reason, MaxReasonRunes); err != nil {
		t.Fatalf("600 runes must fit the governance cap: %v", err)
	}
	if _, err := NormalizeReason(reason, MaxModerationReasonRunes); !errors.Is(err, ErrInvalid) {
		t.Fatalf("600 runes must exceed the moderation cap, got %v", err)
	}
}

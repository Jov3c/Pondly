package moderation

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type Decision struct {
	Publish bool
	Reason  string
}

var htmlTag = regexp.MustCompile(`(?i)<\s*/?\s*[a-z!][^>]*>`)

// Evaluate is the deterministic Pilot allowlist policy. It never publishes
// content that contains links, HTML or control characters, and non-allowlisted
// Agents always stay pending for a human decision.
func Evaluate(kind, content string, allowlisted bool) Decision {
	return EvaluateWithMedia(kind, content, false, allowlisted)
}

func EvaluateWithMedia(kind, content string, hasMedia, allowlisted bool) Decision {
	max := 8000
	if kind == "comment" {
		max = 4000
	}
	if hasMedia && kind == "post" && content == "" {
		if !allowlisted {
			return Decision{Reason: "not_allowlisted"}
		}
		return Decision{Publish: true, Reason: "pilot_allowlist_media"}
	}
	if n := utf8.RuneCountInString(content); n < 1 || n > max {
		return Decision{Reason: "invalid_length"}
	}
	for _, r := range content {
		if r == '\n' || r == '\t' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return Decision{Reason: "control_character"}
		}
	}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "www.") {
		return Decision{Reason: "url_not_allowed"}
	}
	if htmlTag.MatchString(content) {
		return Decision{Reason: "html_not_allowed"}
	}
	if !allowlisted {
		return Decision{Reason: "not_allowlisted"}
	}
	return Decision{Publish: true, Reason: "pilot_allowlist"}
}

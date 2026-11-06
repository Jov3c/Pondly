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

// Signal is one unsafe property of a text. Every rule lives here so the
// synchronous publish gate and the asynchronous review bot cannot drift apart.
type Signal string

const (
	SignalControl      Signal = "control_character"
	SignalUnsafeMarkup Signal = "unsafe_markup"
	SignalBlockedText  Signal = "blocked_phrase"
	SignalRepetition   Signal = "repetition"
	SignalHTML         Signal = "html"
	SignalLink         Signal = "link"
)

const MaxRunes = 40

var (
	unsafeMarkup = regexp.MustCompile(`(?i)<\s*(script|iframe|object|embed|style)\b|javascript\s*:`)
	htmlMarkup   = regexp.MustCompile(`(?i)<\s*/?\s*[a-z!][^>]*>`)
	externalLink = regexp.MustCompile(`(?i)https?://|www\.`)
	blockedText  = []string{
		"加微信", "加vx", "加qq", "扫码加", "扫码领取", "代开发票",
		"博彩平台", "赌博网站", "裸聊", "约炮", "成人视频", "杀猪盘",
		"刷单", "兼职日结",
	}
)

func TextSignals(text string) []Signal {
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return []Signal{SignalControl}
		}
	}
	lower := strings.ToLower(text)
	var signals []Signal
	if unsafeMarkup.MatchString(lower) {
		signals = append(signals, SignalUnsafeMarkup)
	}
	for _, phrase := range blockedText {
		if strings.Contains(lower, phrase) {
			signals = append(signals, SignalBlockedText)
			break
		}
	}
	if longestRuneRun(text) >= MaxRunes {
		signals = append(signals, SignalRepetition)
	}
	if htmlMarkup.MatchString(lower) {
		signals = append(signals, SignalHTML)
	}
	if externalLink.MatchString(lower) {
		signals = append(signals, SignalLink)
	}
	return signals
}

func longestRuneRun(text string) int {
	longest, current := 0, 1
	var previous rune
	first := true
	for _, r := range text {
		if first {
			previous = r
			first = false
			continue
		}
		if r == previous {
			current++
			if current > longest {
				longest = current
			}
		} else {
			previous = r
			current = 1
		}
	}
	return longest
}

// Evaluate is the deterministic Pilot allowlist policy. It never publishes
// content the review bot would not approve outright.
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
	if !allowlisted {
		return Decision{Reason: "not_allowlisted"}
	}
	for _, signal := range TextSignals(content) {
		switch signal {
		case SignalControl:
			return Decision{Reason: "control_character"}
		case SignalUnsafeMarkup:
			return Decision{Reason: "html_not_allowed"}
		case SignalBlockedText:
			return Decision{Reason: "blocked_phrase"}
		case SignalRepetition:
			return Decision{Reason: "repetition"}
		case SignalHTML:
			return Decision{Reason: "html_not_allowed"}
		case SignalLink:
			return Decision{Reason: "url_not_allowed"}
		}
	}
	return Decision{Publish: true, Reason: "pilot_allowlist"}
}

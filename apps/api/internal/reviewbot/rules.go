package reviewbot

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type Action string

const (
	Approve Action = "approve"
	Reject  Action = "reject"
	Hold    Action = "hold"
)

type Decision struct {
	Action Action
	Reason string
}

const (
	RuleVersion     = "pilot-bot-v1"
	maxPostRunes    = 8000
	maxCommentRunes = 4000
	maxTitleRunes   = 150
	maxRuneRun      = 40
)

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

func ReviewPost(title string, content string, imageCount int) Decision {
	text := strings.TrimSpace(title)
	body := strings.TrimSpace(content)
	if body != "" {
		if text != "" {
			text += "\n"
		}
		text += body
	}
	if imageCount <= 0 && text == "" {
		return Decision{Action: Reject, Reason: "bot:reject:empty"}
	}
	if utf8.RuneCountInString(strings.TrimSpace(title)) > maxTitleRunes {
		return Decision{Action: Reject, Reason: "bot:reject:too_long"}
	}
	if utf8.RuneCountInString(body) > maxPostRunes {
		return Decision{Action: Reject, Reason: "bot:reject:too_long"}
	}
	return reviewText(text)
}

func ReviewComment(content string) Decision {
	text := strings.TrimSpace(content)
	if text == "" {
		return Decision{Action: Reject, Reason: "bot:reject:empty"}
	}
	if utf8.RuneCountInString(text) > maxCommentRunes {
		return Decision{Action: Reject, Reason: "bot:reject:too_long"}
	}
	return reviewText(text)
}

func reviewText(text string) Decision {
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return Decision{Action: Reject, Reason: "bot:reject:control_character"}
		}
	}
	lower := strings.ToLower(text)
	if unsafeMarkup.MatchString(lower) {
		return Decision{Action: Reject, Reason: "bot:reject:unsafe_markup"}
	}
	for _, phrase := range blockedText {
		if strings.Contains(lower, phrase) {
			return Decision{Action: Reject, Reason: "bot:reject:blocked_phrase"}
		}
	}
	if longestRuneRun(text) >= maxRuneRun {
		return Decision{Action: Reject, Reason: "bot:reject:repetition"}
	}
	if htmlMarkup.MatchString(lower) {
		return Decision{Action: Hold, Reason: "bot:hold:html"}
	}
	if externalLink.MatchString(lower) {
		return Decision{Action: Hold, Reason: "bot:hold:link"}
	}
	return Decision{Action: Approve, Reason: "bot:approve:safe"}
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

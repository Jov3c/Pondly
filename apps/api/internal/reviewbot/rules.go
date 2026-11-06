package reviewbot

import (
	"strings"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/moderation"
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
	for _, signal := range moderation.TextSignals(text) {
		switch signal {
		case moderation.SignalControl, moderation.SignalUnsafeMarkup, moderation.SignalBlockedText, moderation.SignalRepetition:
			return Decision{Action: Reject, Reason: "bot:reject:" + string(signal)}
		case moderation.SignalHTML, moderation.SignalLink:
			return Decision{Action: Hold, Reason: "bot:hold:" + string(signal)}
		}
	}
	return Decision{Action: Approve, Reason: "bot:approve:safe"}
}

package reviewbot

import (
	"strings"
	"testing"
)

func TestReviewPostRules(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		body   string
		images int
		action Action
		reason string
	}{
		{name: "safe text", body: "今天读到一段关于上下文窗口的讨论，值得回看。", action: Approve, reason: "bot:approve:safe"},
		{name: "safe title and body", title: "夜间运行", body: "保留三行摘要，第二天再看。", action: Approve, reason: "bot:approve:safe"},
		{name: "emoji", body: "🐟🌿", action: Approve, reason: "bot:approve:safe"},
		{name: "image only", images: 2, action: Approve, reason: "bot:approve:safe"},
		{name: "empty", action: Reject, reason: "bot:reject:empty"},
		{name: "link", body: "参考 https://example.com/post 这里的记录", action: Hold, reason: "bot:hold:link"},
		{name: "www link", body: "来源 www.example.com", action: Hold, reason: "bot:hold:link"},
		{name: "html markup", body: "试试 <div>这段</div>", action: Hold, reason: "bot:hold:html"},
		{name: "script", body: "<script>alert(1)</script>", action: Reject, reason: "bot:reject:unsafe_markup"},
		{name: "javascript uri", body: "javascript:alert(1)", action: Reject, reason: "bot:reject:unsafe_markup"},
		{name: "control character", body: "正常\x01内容", action: Reject, reason: "bot:reject:control_character"},
		{name: "blocked phrase", body: "需要资料可以加微信", action: Reject, reason: "bot:reject:blocked_phrase"},
		{name: "blocked phrase in title", title: "扫码领取资料", body: "正文", action: Reject, reason: "bot:reject:blocked_phrase"},
		{name: "repetition", body: strings.Repeat("哈", 40), action: Reject, reason: "bot:reject:repetition"},
		{name: "below repetition threshold", body: strings.Repeat("哈", 39), action: Approve, reason: "bot:approve:safe"},
		{name: "too long body", body: strings.Repeat("字", maxPostRunes+1), action: Reject, reason: "bot:reject:too_long"},
		{name: "too long title", title: strings.Repeat("字", maxTitleRunes+1), body: "正文", action: Reject, reason: "bot:reject:too_long"},
		{name: "newlines and tabs", body: "第一行\n第二行\t结束", action: Approve, reason: "bot:approve:safe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReviewPost(tc.title, tc.body, tc.images)
			if got.Action != tc.action || got.Reason != tc.reason {
				t.Fatalf("got %+v, want %s %s", got, tc.action, tc.reason)
			}
		})
	}
}

func TestReviewCommentRules(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		action Action
		reason string
	}{
		{name: "safe comment", body: "我会先复述对方最后一句，再补充自己的判断。", action: Approve, reason: "bot:approve:safe"},
		{name: "empty comment", body: "  \n\t", action: Reject, reason: "bot:reject:empty"},
		{name: "link comment", body: "完整记录见 https://example.com/note", action: Hold, reason: "bot:hold:link"},
		{name: "html comment", body: "<b>强调</b>", action: Hold, reason: "bot:hold:html"},
		{name: "spam comment", body: "兼职日结，扫码领取", action: Reject, reason: "bot:reject:blocked_phrase"},
		{name: "long comment", body: strings.Repeat("字", maxCommentRunes+1), action: Reject, reason: "bot:reject:too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReviewComment(tc.body)
			if got.Action != tc.action || got.Reason != tc.reason {
				t.Fatalf("got %+v, want %s %s", got, tc.action, tc.reason)
			}
		})
	}
}

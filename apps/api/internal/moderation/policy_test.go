package moderation

import "testing"

func TestEvaluatePureMedia(t *testing.T) {
	if got := EvaluateWithMedia("post", "", true, true); !got.Publish || got.Reason != "pilot_allowlist_media" {
		t.Fatalf("allowlisted pure media: %+v", got)
	}
	if got := EvaluateWithMedia("post", "", true, false); got.Publish || got.Reason != "not_allowlisted" {
		t.Fatalf("non-allowlisted pure media: %+v", got)
	}
	if got := Evaluate("post", "", true); got.Publish || got.Reason != "invalid_length" {
		t.Fatalf("empty text without media: %+v", got)
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		content     string
		allowlisted bool
		publish     bool
		reason      string
	}{
		{"safe allowlisted", "post", "今天在水里遇到了一条新鱼。", true, true, "pilot_allowlist"},
		{"not allowlisted", "post", "safe content", false, false, "not_allowlisted"},
		{"url", "post", "see https://example.com", true, false, "url_not_allowed"},
		{"www", "post", "see www.example.com", true, false, "url_not_allowed"},
		{"html", "post", "hello <script>alert(1)</script>", true, false, "html_not_allowed"},
		{"control", "post", "hello\x00world", true, false, "control_character"},
		{"newline allowed", "comment", "line one\nline two", true, true, "pilot_allowlist"},
		{"empty", "post", "", true, false, "invalid_length"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.kind, tc.content, tc.allowlisted)
			if got.Publish != tc.publish || got.Reason != tc.reason {
				t.Fatalf("got %+v, want publish=%v reason=%s", got, tc.publish, tc.reason)
			}
		})
	}
}

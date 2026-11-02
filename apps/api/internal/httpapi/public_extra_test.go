package httpapi

import (
	"bytes"
	"context"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func TestPublicAgentsStatsAndProfileHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	agentID, token := seedActiveAgent(t, db, "public-http@example.com", "publichttp")
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePost(context.Background(), agentID, "agent.post.create",
		"public-http-post-key", [32]byte{6}, store.PostInput{PostType: "moment", Content: "public"}, 10); err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "GET", "/api/v1/public/agents", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(agentID.String())) {
		t.Fatalf("public agents: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/stats", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte(`"agents_total":1`)) ||
		!bytes.Contains(resp.Body, []byte(`"posts_published":1`)) {
		t.Fatalf("public stats: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PATCH", "/api/v1/agent/me", map[string]string{
		"display_name": "新昵称", "bio": "新的简介",
	}, token, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["display_name"] != "新昵称" {
		t.Fatalf("profile update: %d %s", resp.Code, resp.Body)
	}
}

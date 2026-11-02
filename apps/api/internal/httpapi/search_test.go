package httpapi

import (
	"bytes"
	"context"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func TestPublicSearchHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	agentID, _ := seedActiveAgent(t, db, "search-http@example.com", "searchhttp")
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePost(context.Background(), agentID, "agent.post.create",
		"search-http-key-1", [32]byte{9}, store.PostInput{PostType: "moment", Content: "池塘里的星光"}, 10); err != nil {
		t.Fatal(err)
	}
	resp := callAPI(t, h, "GET", "/api/v1/public/search?q=%E6%98%9F%E5%85%89", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("池塘里的星光")) {
		t.Fatalf("search: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/search?q=", nil, "", "", "", "")
	if resp.Code != 422 {
		t.Fatalf("empty search: %d %s", resp.Code, resp.Body)
	}
}

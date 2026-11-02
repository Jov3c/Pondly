package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type feedPageEnvelope struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Meta struct {
		Page    int  `json:"page"`
		HasMore bool `json:"has_more"`
	} `json:"meta"`
}

func TestPublicFeedPagePagination(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	alice, token := seedActiveAgent(t, db, "feed-page@example.com", "feedpage")
	if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, alice); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		resp := callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
			"post_type": "moment", "content": fmt.Sprintf("page post %d", i),
		}, token, "", "", fmt.Sprintf("feed-page-post-%04d", i))
		if resp.Code != 201 {
			t.Fatalf("create post %d: %d %s", i, resp.Code, resp.Body)
		}
	}

	var first feedPageEnvelope
	resp := callAPI(t, h, "GET", "/api/v1/public/feed?limit=2&page=1", nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("page 1: %d %s", resp.Code, resp.Body)
	}
	if err := json.Unmarshal(resp.Body, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 2 || !first.Meta.HasMore || first.Meta.Page != 1 {
		t.Fatalf("unexpected page 1: %s", resp.Body)
	}

	var second feedPageEnvelope
	resp = callAPI(t, h, "GET", "/api/v1/public/feed?limit=2&page=2", nil, "", "", "", "")
	if resp.Code != 200 {
		t.Fatalf("page 2: %d %s", resp.Code, resp.Body)
	}
	if err := json.Unmarshal(resp.Body, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Data) != 1 || second.Meta.HasMore || second.Meta.Page != 2 {
		t.Fatalf("unexpected page 2: %s", resp.Body)
	}
	if first.Data[0].ID == second.Data[0].ID || first.Data[1].ID == second.Data[0].ID {
		t.Fatalf("page overlap: %+v %+v", first.Data, second.Data)
	}

	for _, path := range []string{
		"/api/v1/public/feed?page=0",
		"/api/v1/public/feed?page=abc",
		"/api/v1/public/feed?page=1&cursor=abc",
	} {
		resp = callAPI(t, h, "GET", path, nil, "", "", "", "")
		if resp.Code != 422 {
			t.Fatalf("%s: %d %s", path, resp.Code, resp.Body)
		}
	}
}

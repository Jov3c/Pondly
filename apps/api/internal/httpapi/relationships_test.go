package httpapi

import (
	"context"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
)

func TestReactionsAndFollowsHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	alice, _ := seedActiveAgent(t, db, "rel-alice@example.com", "relalice")
	bob, bobToken := seedActiveAgent(t, db, "rel-bob@example.com", "relbob")
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, alice); err != nil {
		t.Fatal(err)
	}
	post, err := db.CreatePost(context.Background(), alice, "agent.post.create",
		"relationship-post-key", [32]byte{3}, store.PostInput{PostType: "moment", Content: "react"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "PUT", "/api/v1/agent/posts/"+post.ID.String()+"/reaction",
		map[string]string{"type": "like"}, bobToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["reaction_count"] != float64(1) {
		t.Fatalf("put reaction: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/posts/"+post.ID.String()+"/reaction",
		map[string]string{"type": "like"}, bobToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["reaction_count"] != float64(1) {
		t.Fatalf("duplicate reaction: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+post.ID.String(), nil, "", "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["reaction_count"] != float64(1) {
		t.Fatalf("public reaction count: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/posts/"+post.ID.String()+"/reaction", nil, bobToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["reaction_count"] != float64(0) {
		t.Fatalf("delete reaction: %d %s", resp.Code, resp.Body)
	}

	resp = callAPI(t, h, "PUT", "/api/v1/agent/follows/"+alice.String(), nil, bobToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["id"] != alice.String() {
		t.Fatalf("follow: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "PUT", "/api/v1/agent/follows/"+bob.String(), nil, bobToken, "", "", "")
	if resp.Code != 422 {
		t.Fatalf("self follow: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "DELETE", "/api/v1/agent/follows/"+alice.String(), nil, bobToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("unfollow: %d %s", resp.Code, resp.Body)
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func TestAgentHomeAndAckHTTP(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	alice, aliceToken := seedActiveAgent(t, db, "home-http-alice@example.com", "homehttpalice")
	bob, _ := seedActiveAgent(t, db, "home-http-bob@example.com", "homehttpbob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := db.Pool.Exec(context.Background(),
			`INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := db.CreatePost(context.Background(), alice, "agent.post.create",
		"home-http-post-key", [32]byte{7}, store.PostInput{PostType: "moment", Content: "home"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateComment(context.Background(), bob, post.ID, "agent.comment.create",
		"home-http-comment-key", [32]byte{8}, store.CommentInput{Content: "reply"}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ProcessOutboxBatch(context.Background(), "home-http-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "GET", "/api/v1/agent/home", nil, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("home: %d %s", resp.Code, resp.Body)
	}
	home := dataMap(t, resp)
	if home["unread_count"] != float64(1) || home["events_cursor"] == nil {
		t.Fatalf("home data: %+v", home)
	}
	quota := home["quota"].(map[string]any)
	if quota["posts"] != float64(9) || quota["comments"] != float64(30) {
		t.Fatalf("home quota: %+v", quota)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, aliceToken, "", "", "")
	var events struct {
		Data []store.Event `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &events); err != nil {
		t.Fatal(err)
	}
	if len(events.Data) != 1 {
		t.Fatalf("events: %s", resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/events/ack", map[string]any{
		"event_ids": []string{events.Data[0].ID.String()},
	}, aliceToken, "", "", "home-ack-key-0000001")
	if resp.Code != 200 || dataMap(t, resp)["acknowledged"] != float64(1) {
		t.Fatalf("ack: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "POST", "/api/v1/agent/events/ack", map[string]any{
		"event_ids": []string{events.Data[0].ID.String()},
	}, aliceToken, "", "", "home-ack-key-0000002")
	if resp.Code != 200 || dataMap(t, resp)["acknowledged"] != float64(0) {
		t.Fatalf("repeat ack: %d %s", resp.Code, resp.Body)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/home", nil, aliceToken, "", "", "")
	if resp.Code != 200 || dataMap(t, resp)["unread_count"] != float64(0) {
		t.Fatalf("home after ack: %d %s", resp.Code, resp.Body)
	}
}

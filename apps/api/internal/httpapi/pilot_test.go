package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

type eventsEnvelope struct {
	Data []store.Event `json:"data"`
	Meta struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
}

func TestPilotTwoAgentLoop(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	alice, aliceToken := seedActiveAgent(t, db, "loop-alice@example.com", "loopalice")
	bob, bobToken := seedActiveAgent(t, db, "loop-bob@example.com", "loopbob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO follows (follower_agent_id, followee_agent_id)
		VALUES ($1, $2)`, bob, alice); err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "POST", "/api/v1/agent/posts", map[string]any{
		"post_type": "moment", "content": "alice starts a thread",
	}, aliceToken, "", "", "loop-post-key-0001")
	if resp.Code != 201 {
		t.Fatalf("alice post: %d %s", resp.Code, resp.Body)
	}
	postID := dataMap(t, resp)["id"].(string)
	if _, err := db.ProcessOutboxBatch(ctx, "loop-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, bobToken, "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("post.published")) {
		t.Fatalf("bob post event: %d %s", resp.Code, resp.Body)
	}
	var bobEvents eventsEnvelope
	if err := json.Unmarshal(resp.Body, &bobEvents); err != nil {
		t.Fatal(err)
	}
	bobCursor := bobEvents.Meta.NextCursor

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts/"+postID+"/comments", map[string]any{
		"content": "bob replies to alice",
	}, bobToken, "", "", "loop-comment-key-01")
	if resp.Code != 201 {
		t.Fatalf("bob comment: %d %s", resp.Code, resp.Body)
	}
	bobCommentID := dataMap(t, resp)["id"].(string)
	if _, err := db.ProcessOutboxBatch(ctx, "loop-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events", nil, aliceToken, "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("comment.published")) {
		t.Fatalf("alice comment event: %d %s", resp.Code, resp.Body)
	}
	var aliceEvents eventsEnvelope
	if err := json.Unmarshal(resp.Body, &aliceEvents); err != nil {
		t.Fatal(err)
	}
	aliceCursor := aliceEvents.Meta.NextCursor

	resp = callAPI(t, h, "POST", "/api/v1/agent/posts/"+postID+"/comments", map[string]any{
		"content": "alice answers bob", "parent_id": bobCommentID,
	}, aliceToken, "", "", "loop-reply-key-0001")
	if resp.Code != 201 {
		t.Fatalf("alice reply: %d %s", resp.Code, resp.Body)
	}
	if _, err := db.ProcessOutboxBatch(ctx, "loop-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events?cursor="+bobCursor, nil, bobToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("bob reply event: %d %s", resp.Code, resp.Body)
	}
	var bobNext eventsEnvelope
	if err := json.Unmarshal(resp.Body, &bobNext); err != nil {
		t.Fatal(err)
	}
	if len(bobNext.Data) != 1 || bobNext.Data[0].Type != "comment.published" || bobNext.Data[0].InboxSeq != 2 {
		t.Fatalf("bob next events: %+v", bobNext)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events?cursor="+aliceCursor, nil, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("alice next events: %d %s", resp.Code, resp.Body)
	}
	var aliceNext eventsEnvelope
	if err := json.Unmarshal(resp.Body, &aliceNext); err != nil {
		t.Fatal(err)
	}
	if len(aliceNext.Data) != 0 {
		t.Fatalf("alice should not receive her own reply: %+v", aliceNext)
	}

	resp = callAPI(t, h, "GET", "/api/v1/public/posts/"+postID+"/comments", nil, "", "", "", "")
	if resp.Code != 200 || !bytes.Contains(resp.Body, []byte("alice answers bob")) {
		t.Fatalf("public thread: %d %s", resp.Code, resp.Body)
	}
}

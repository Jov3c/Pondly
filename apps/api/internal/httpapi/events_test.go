package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/store"
	"github.com/google/uuid"
)

func seedActiveAgent(t *testing.T, db *store.Store, email, handle string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	codeHash := sha256.Sum256([]byte("123456"))
	if err := db.CreateOwnerOTP(ctx, email, codeHash, "sign_in", nil, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	ownerID, err := db.VerifyOwnerOTP(ctx, email, codeHash, "sign_in", nil, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := db.CreateEnrollmentTicket(ctx, ownerID, "owner.enrollment.create",
		"seed-enroll-"+email, sha256.Sum256([]byte("enroll-"+email)), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := db.RegisterAgent(ctx, "agent.register", "seed-register-"+email,
		sha256.Sum256([]byte("register-"+email)),
		store.RegistrationInput{Handle: handle, EnrollmentTicket: enrollment.EnrollmentTicket}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Agent.ID, reg.Token
}

func TestEventsHTTPCursorAndGone(t *testing.T) {
	h, _, db, _ := newIdentityApp(t)
	ctx := context.Background()
	alice, aliceToken := seedActiveAgent(t, db, "events-alice@example.com", "eventsalice")
	bob, _ := seedActiveAgent(t, db, "events-bob@example.com", "eventsbob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := db.CreatePost(ctx, alice, "agent.post.create", "events-post-key-01",
		sha256.Sum256([]byte("events-post")), store.PostInput{PostType: "moment", Content: "events"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"one", "two"} {
		if _, err := db.CreateComment(ctx, bob, post.ID, "agent.comment.create",
			"events-comment-key-"+string(rune('a'+i)), sha256.Sum256([]byte(text)),
			store.CommentInput{Content: text}, 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ProcessOutboxBatch(ctx, "test-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	resp := callAPI(t, h, "GET", "/api/v1/agent/events?limit=1", nil, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("events: %d %s", resp.Code, resp.Body)
	}
	var envelope struct {
		Data []store.Event `json:"data"`
		Meta struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].Type != "comment.published" ||
		!envelope.Meta.HasMore || envelope.Meta.NextCursor == "" {
		t.Fatalf("events envelope: %+v", envelope)
	}
	cursor := envelope.Meta.NextCursor

	resp = callAPI(t, h, "GET", "/api/v1/agent/events?cursor="+url.QueryEscape(cursor), nil, aliceToken, "", "", "")
	if resp.Code != 200 {
		t.Fatalf("events next: %d %s", resp.Code, resp.Body)
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Meta.HasMore {
		t.Fatalf("events second page: %+v", envelope)
	}

	resp = callAPI(t, h, "GET", "/api/v1/agent/events?cursor="+url.QueryEscape(cursor+"x"), nil, aliceToken, "", "", "")
	if resp.Code != 400 || !bytes.Contains(resp.Body, []byte("invalid_cursor")) {
		t.Fatalf("tampered cursor: %d %s", resp.Code, resp.Body)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE notifications SET expires_at = $2 WHERE recipient_agent_id = $1`,
		alice, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	resp = callAPI(t, h, "GET", "/api/v1/agent/events?cursor="+url.QueryEscape(cursor), nil, aliceToken, "", "", "")
	if resp.Code != 410 || !bytes.Contains(resp.Body, []byte("cursor_expired")) ||
		!bytes.Contains(resp.Body, []byte("restart_cursor")) {
		t.Fatalf("expired cursor: %d %s", resp.Code, resp.Body)
	}
}

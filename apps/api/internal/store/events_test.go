package store

import (
	"context"
	"testing"
	"time"
)

func TestOutboxEventsOrderDedupeAndRetention(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "alice@example.com", "alice")
	bob, _, _ := createActiveAgent(t, s, "bob@example.com", "bob")
	for _, id := range []string{alice.String(), bob.String()} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO follows (follower_agent_id, followee_agent_id)
		VALUES ($1, $2)`, bob, alice); err != nil {
		t.Fatal(err)
	}

	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("alice-post"),
		PostInput{PostType: "moment", Content: "hello from alice"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ProcessOutboxBatch(ctx, "worker-1", 20, 100); err != nil || n != 1 {
		t.Fatalf("post outbox: n=%d err=%v", n, err)
	}
	bobPage, err := s.GetAgentEvents(ctx, bob, 0, 20)
	if err != nil || len(bobPage.Events) != 1 || bobPage.Events[0].Type != "post.published" || bobPage.Events[0].InboxSeq != 1 {
		t.Fatalf("bob post event: %+v %v", bobPage, err)
	}
	alicePage, err := s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || len(alicePage.Events) != 0 {
		t.Fatalf("alice must not receive own post: %+v %v", alicePage, err)
	}

	first, err := s.CreateComment(ctx, bob, post.ID, "agent.comment.create", idemKey(), requestHash("bob-c1"),
		CommentInput{Content: "first"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, bob, post.ID, "agent.comment.create", idemKey(), requestHash("bob-c2"),
		CommentInput{Content: "second"}, 10); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ProcessOutboxBatch(ctx, "worker-1", 20, 100); err != nil || n != 2 {
		t.Fatalf("comment outbox: n=%d err=%v", n, err)
	}
	alicePage, err = s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || len(alicePage.Events) != 2 {
		t.Fatalf("alice comments: %+v %v", alicePage, err)
	}
	if alicePage.Events[0].InboxSeq != 1 || alicePage.Events[1].InboxSeq != 2 {
		t.Fatalf("seq order: %+v", alicePage.Events)
	}
	if alicePage.Events[0].Type != "comment.published" || alicePage.Events[0].ConversationID == nil ||
		*alicePage.Events[0].ConversationID != post.ID {
		t.Fatalf("event shape: %+v", alicePage.Events[0])
	}

	if _, err := s.CreateComment(ctx, alice, post.ID, "agent.comment.create", idemKey(), requestHash("alice-reply"),
		CommentInput{Content: "reply", ParentID: &first.ID}, 10); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ProcessOutboxBatch(ctx, "worker-1", 20, 100); err != nil || n != 1 {
		t.Fatalf("reply outbox: n=%d err=%v", n, err)
	}
	bobPage, err = s.GetAgentEvents(ctx, bob, 0, 20)
	if err != nil || len(bobPage.Events) != 2 || bobPage.Events[1].Type != "comment.published" {
		t.Fatalf("bob reply event: %+v %v", bobPage, err)
	}

	// Rerunning the same Outbox event must not duplicate the notification.
	var eventID string
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM outbox_events ORDER BY created_at ASC LIMIT 1`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE outbox_events SET processed_at = NULL WHERE id = $1`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "worker-2", 20, 100); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE recipient_agent_id = $1`, bob).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("dedupe count = %d", count)
	}

	// Expire Alice's notifications and request an older cursor -> gap.
	if _, err := s.Pool.Exec(ctx, `UPDATE notifications SET expires_at = $2 WHERE recipient_agent_id = $1`,
		alice, s.now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	gap, err := s.GetAgentEvents(ctx, alice, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !gap.CursorGap || gap.LowestRetained != 3 || gap.LastSeq != 2 {
		t.Fatalf("gap page: %+v", gap)
	}
	firstAfterGap, err := s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || firstAfterGap.CursorGap {
		t.Fatalf("first fetch after gap: %+v %v", firstAfterGap, err)
	}
}

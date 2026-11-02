package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestHomeUnreadAndAcknowledgement(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "home-alice@example.com", "homealice")
	bob, _, _ := createActiveAgent(t, s, "home-bob@example.com", "homebob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("home-post"),
		PostInput{PostType: "moment", Content: "home event"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, bob, post.ID, "agent.comment.create", idemKey(), requestHash("home-comment"),
		CommentInput{Content: "home reply"}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "home-worker", 20, 100); err != nil {
		t.Fatal(err)
	}

	home, err := s.GetAgentHome(ctx, alice, 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if home.UnreadCount != 1 || home.LastSeq != 1 {
		t.Fatalf("home: %+v", home)
	}
	if home.Quota["posts"] != 9 || home.Quota["comments"] != 30 {
		t.Fatalf("quota: %+v", home.Quota)
	}
	page, err := s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("events: %+v %v", page, err)
	}
	eventID := page.Events[0].ID

	if n, err := s.AcknowledgeEvents(ctx, bob, []uuid.UUID{eventID}); err != nil || n != 0 {
		t.Fatalf("other recipient ack: n=%d err=%v", n, err)
	}
	if n, err := s.AcknowledgeEvents(ctx, alice, []uuid.UUID{eventID}); err != nil || n != 1 {
		t.Fatalf("ack: n=%d err=%v", n, err)
	}
	if n, err := s.AcknowledgeEvents(ctx, alice, []uuid.UUID{eventID}); err != nil || n != 0 {
		t.Fatalf("repeat ack: n=%d err=%v", n, err)
	}
	home, err = s.GetAgentHome(ctx, alice, 10, 30)
	if err != nil || home.UnreadCount != 0 {
		t.Fatalf("home after ack: %+v %v", home, err)
	}
}

func TestFollowAndReactionNotifications(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "notify-alice@example.com", "notifyalice")
	bob, _, _ := createActiveAgent(t, s, "notify-bob@example.com", "notifybob")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, alice); err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("notify-post"),
		PostInput{PostType: "moment", Content: "notify post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "notify-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowAgent(ctx, bob, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutReaction(ctx, bob, post.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ProcessOutboxBatch(ctx, "notify-worker", 20, 100); err != nil || n != 2 {
		t.Fatalf("follow/reaction outbox: n=%d err=%v", n, err)
	}
	page, err := s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("alice notifications: %+v %v", page, err)
	}
	types := map[string]bool{}
	for _, event := range page.Events {
		types[event.Type] = true
		if event.Type == "reaction.updated" && (event.ConversationID == nil || *event.ConversationID != post.ID) {
			t.Fatalf("reaction conversation: %+v", event)
		}
	}
	if !types["follow.created"] || !types["reaction.updated"] {
		t.Fatalf("event types: %+v", types)
	}

	if _, err := s.FollowAgent(ctx, bob, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutReaction(ctx, bob, post.ID, "like"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ProcessOutboxBatch(ctx, "notify-worker", 20, 100); err != nil || n != 0 {
		t.Fatalf("duplicate outbox: n=%d err=%v", n, err)
	}
	page, err = s.GetAgentEvents(ctx, alice, 0, 20)
	if err != nil || len(page.Events) != 2 {
		t.Fatalf("duplicate notifications: %+v %v", page, err)
	}
}

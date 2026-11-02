package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
)

func createActiveAgent(t *testing.T, s *Store, email, handle string) (uuid.UUID, string, uuid.UUID) {
	t.Helper()
	ownerID := mustOwner(t, s, email)
	enrollment, err := s.CreateEnrollmentTicket(context.Background(), ownerID,
		"owner.enrollment.create", idemKey(), requestHash("enroll-"+handle), 15*time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.RegisterAgent(context.Background(), "agent.register", idemKey(), requestHash("register-"+handle),
		RegistrationInput{Handle: handle, EnrollmentTicket: enrollment.EnrollmentTicket}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Agent.ID, reg.Token, ownerID
}

func TestPostModerationQuotaAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "poster@example.com", "poster")

	// Not allowlisted: pending, invisible.
	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("p1"),
		PostInput{PostType: "moment", Content: "hello pond"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if post.Status != "pending_review" {
		t.Fatalf("status = %s", post.Status)
	}
	if _, err := s.PublicPost(ctx, post.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending post public: %v", err)
	}

	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	key := idemKey()
	body := requestHash("p2")
	published, err := s.CreatePost(ctx, agentID, "agent.post.create", key, body,
		PostInput{PostType: "discussion", Title: stringPtr("A topic"), Content: "real content"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != "published" {
		t.Fatalf("published status = %s", published.Status)
	}
	replay, err := s.CreatePost(ctx, agentID, "agent.post.create", key, body,
		PostInput{PostType: "discussion", Title: stringPtr("A topic"), Content: "real content"}, 2)
	if err != nil || replay.ID != published.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	var outbox int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type='post.published'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("outbox = %d, want 1", outbox)
	}

	// Daily quota: the first pending post + this published post already hit 2.
	if _, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("p3"),
		PostInput{PostType: "moment", Content: "third"}, 2); !errors.Is(err, ErrQuota) {
		t.Fatalf("expected quota, got %v", err)
	}

	// URL is unsafe even for an allowlisted agent and stays pending.
	unsafe, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("p4"),
		PostInput{PostType: "moment", Content: "see https://example.com"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if unsafe.Status != "pending_review" {
		t.Fatalf("unsafe status = %s", unsafe.Status)
	}

	// Discussion requires a title.
	if _, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("p5"),
		PostInput{PostType: "discussion", Content: "no title"}, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("discussion without title: %v", err)
	}
}

func TestCommentsDepthAndPublicVisibility(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "commenter@example.com", "commenter")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("post"),
		PostInput{PostType: "moment", Content: "topic"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(), requestHash("c1"),
		CommentInput{Content: "first reply"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if comment.Depth != 0 || comment.Status != "published" {
		t.Fatalf("comment: %+v", comment)
	}
	reply, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(), requestHash("c2"),
		CommentInput{Content: "reply", ParentID: &comment.ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Depth != 1 {
		t.Fatalf("reply depth = %d", reply.Depth)
	}
	comments, _, _, err := s.PublicComments(ctx, post.ID, "", 20)
	if err != nil || len(comments) != 2 {
		t.Fatalf("public comments: %+v %v", comments, err)
	}

	// Pending comment is not public and cannot be a parent.
	if _, err := s.Pool.Exec(ctx, `UPDATE comments SET status='pending_review' WHERE id=$1`, reply.ID); err != nil {
		t.Fatal(err)
	}
	comments, _, _, err = s.PublicComments(ctx, post.ID, "", 20)
	if err != nil || len(comments) != 1 {
		t.Fatalf("pending comment leaked: %+v %v", comments, err)
	}
	if _, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(), requestHash("c3"),
		CommentInput{Content: "child", ParentID: &reply.ID}, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pending parent accepted: %v", err)
	}
}

func TestPublicFeedCursor(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "feed@example.com", "feed")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		p, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("feed"+string(rune('a'+i))),
			PostInput{PostType: "moment", Content: "post"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	first, next, hasMore, err := s.PublicFeed(ctx, "", 2, nil, "")
	if err != nil || len(first) != 2 || !hasMore || next == "" {
		t.Fatalf("first page: %d %q %v %v", len(first), next, hasMore, err)
	}
	second, next2, hasMore2, err := s.PublicFeed(ctx, next, 2, nil, "")
	if err != nil || len(second) != 1 || hasMore2 || next2 != "" {
		t.Fatalf("second page: %d %q %v %v", len(second), next2, hasMore2, err)
	}
	if first[0].ID == second[0].ID || first[1].ID == second[0].ID {
		t.Fatal("cursor duplicated a post")
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range append(first, second...) {
		if seen[p.ID] {
			t.Fatal("duplicate post")
		}
		seen[p.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("seen = %d", len(seen))
	}
	if _, _, _, err := s.PublicFeed(ctx, "not-a-cursor", 2, nil, ""); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor: %v", err)
	}
}

func TestDeleteMyPost(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	author, _, _ := createActiveAgent(t, s, "delete-author@example.com", "deleteauthor")
	other, _, _ := createActiveAgent(t, s, "delete-other@example.com", "deleteother")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, author); err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(ctx, author, "agent.post.create", idemKey(), requestHash("delete-post"),
		PostInput{PostType: "moment", Content: "delete me"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteMyPost(ctx, other, post.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other delete: %v", err)
	}
	deleted, err := s.DeleteMyPost(ctx, author, post.ID)
	if err != nil || deleted.Status != "deleted" {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	if _, err := s.PublicPost(ctx, post.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted post public: %v", err)
	}
	if _, err := s.DeleteMyPost(ctx, author, post.ID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
}

func TestConversationCooldown(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "cool-alice@example.com", "coolalice")
	bob, _, _ := createActiveAgent(t, s, "cool-bob@example.com", "coolbob")
	for _, id := range []uuid.UUID{alice, bob} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("cool-post"),
		PostInput{PostType: "moment", Content: "cool post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		author := bob
		if i%2 == 1 {
			author = alice
		}
		if _, err := s.CreateComment(ctx, author, post.ID, "agent.comment.create",
			idemKey(), requestHash("cool-"+string(rune('a'+i))),
			CommentInput{Content: "cool reply"}, 100); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateComment(ctx, bob, post.ID, "agent.comment.create",
		idemKey(), requestHash("cool-blocked"), CommentInput{Content: "one too many"}, 100); !errors.Is(err, ErrCooldown) {
		t.Fatalf("expected cooldown, got %v", err)
	}
}

func TestConcurrentIdempotentPost(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "idempotent@example.com", "idempotent")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	const workers = 100
	key := "concurrent-idem-key-01"
	hash := requestHash("same post")
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := s.CreatePost(ctx, agentID, "agent.post.create", key, hash,
				PostInput{PostType: "moment", Content: "same content"}, 1000)
			errCh <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	var posts, outbox, actions, used int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM moderation_actions`).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT used FROM daily_write_counters WHERE agent_id=$1 AND operation='post.create'`, agentID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || outbox != 1 || actions != 1 || used != 1 {
		t.Fatalf("posts=%d outbox=%d actions=%d used=%d", posts, outbox, actions, used)
	}
}

func TestPublicAgentAndTokenLookup(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, token, _ := createActiveAgent(t, s, "visible@example.com", "visible")
	agent, err := s.PublicAgent(ctx, agentID)
	if err != nil || agent.ID != agentID {
		t.Fatalf("public agent: %+v %v", agent, err)
	}
	authAgent, err := s.GetAgentByTokenHash(ctx, auth.HashToken(token))
	if err != nil || authAgent.Agent.ID != agentID {
		t.Fatalf("token lookup: %+v %v", authAgent, err)
	}
}

func stringPtr(s string) *string { return &s }

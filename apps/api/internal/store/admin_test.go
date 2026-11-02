package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
)

func createAdmin(t *testing.T, s *Store) (uuid.UUID, uuid.UUID, string, string) {
	t.Helper()
	passwordHash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateAdminAccount(context.Background(), "admin-"+uuid.NewString()[:8], passwordHash, secret)
	if err != nil {
		t.Fatal(err)
	}
	return id, uuid.Nil, passwordHash, secret
}

func TestPendingReviewQueue(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	adminID, _, _, _ := createAdmin(t, s)
	agentID, _, _ := createActiveAgent(t, s, "queue@example.com", "queueagent")

	pendingPost, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("queue-post"),
		PostInput{PostType: "moment", Content: "post waiting for review"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	baseTitle := "base discussion"
	basePost, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("queue-base"),
		PostInput{PostType: "discussion", Title: &baseTitle, Content: "base published post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminDecidePost(ctx, adminID, basePost.ID, "approve", "publish base post",
		requestHash("queue-approve"), "admin.post.decision", idemKey()); err != nil {
		t.Fatal(err)
	}
	pendingComment, err := s.CreateComment(ctx, agentID, basePost.ID, "agent.comment.create", idemKey(),
		requestHash("queue-comment"), CommentInput{Content: "comment waiting for review"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	queue, err := s.ListPendingReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Posts) != 1 || queue.Posts[0].ID != pendingPost.ID || queue.Posts[0].Content != "post waiting for review" {
		t.Fatalf("pending posts: %+v", queue.Posts)
	}
	if len(queue.Comments) != 1 || queue.Comments[0].ID != pendingComment.ID ||
		queue.Comments[0].PostID != basePost.ID || queue.Comments[0].PostTitle == nil {
		t.Fatalf("pending comments: %+v", queue.Comments)
	}
	if queue.Posts[0].AuthorHandle != "queueagent" {
		t.Fatalf("pending post author: %+v", queue.Posts[0])
	}

	if _, err := s.AdminDecideComment(ctx, adminID, pendingComment.ID, "approve", "reviewed comment",
		requestHash("queue-comment-approve"), "admin.comment.decision", idemKey()); err != nil {
		t.Fatal(err)
	}
	queue, err = s.ListPendingReview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Comments) != 0 || len(queue.Posts) != 1 {
		t.Fatalf("queue after approval: %+v", queue)
	}
}

func TestAdminModerationDecisions(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	adminID, _, _, _ := createAdmin(t, s)
	agentID, _, _ := createActiveAgent(t, s, "moderated@example.com", "moderated")

	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("pending-post"),
		PostInput{PostType: "moment", Content: "needs review"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if post.Status != "pending_review" {
		t.Fatalf("post status = %s", post.Status)
	}
	approved, err := s.AdminDecidePost(ctx, adminID, post.ID, "approve", "pilot test approval",
		requestHash("decision"), "admin.post.decision", idemKey())
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "published" || approved.PublishedAt == nil {
		t.Fatalf("approved post: %+v", approved)
	}
	replay, err := s.AdminDecidePost(ctx, adminID, post.ID, "approve", "pilot test approval",
		requestHash("decision"), "admin.post.decision", "different-key-00001")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second decision must conflict: %+v %v", replay, err)
	}
	var outbox int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type='post.published'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("post outbox = %d", outbox)
	}
	var actions int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM moderation_actions WHERE target_id=$1 AND actor_id=$2`, post.ID, adminID).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 1 {
		t.Fatalf("moderation actions = %d", actions)
	}

	comment, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(), requestHash("pending-comment"),
		CommentInput{Content: "needs review too"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if comment.Status != "pending_review" {
		t.Fatalf("comment status = %s", comment.Status)
	}
	approvedComment, err := s.AdminDecideComment(ctx, adminID, comment.ID, "approve", "pilot comment approval",
		requestHash("comment-decision"), "admin.comment.decision", idemKey())
	if err != nil {
		t.Fatal(err)
	}
	if approvedComment.Status != "published" {
		t.Fatalf("approved comment: %+v", approvedComment)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type='comment.published'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("comment outbox = %d", outbox)
	}

	rejected, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("reject-post"),
		PostInput{PostType: "moment", Content: "reject me"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminDecidePost(ctx, adminID, rejected.ID, "reject", "policy violation",
		requestHash("reject-decision"), "admin.post.decision", idemKey()); err != nil {
		t.Fatal(err)
	}
	var rejectedStatus string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM posts WHERE id=$1`, rejected.ID).Scan(&rejectedStatus); err != nil {
		t.Fatal(err)
	}
	if rejectedStatus != "rejected" {
		t.Fatalf("rejected status = %s", rejectedStatus)
	}
	var before int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 2 {
		t.Fatalf("outbox after reject = %d, want 2", before)
	}
}

func TestAdminHidesPublishedContent(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	adminID, _, _, _ := createAdmin(t, s)
	agentID, _, _ := createActiveAgent(t, s, "hide@example.com", "hidefish")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("hide-post"),
		PostInput{PostType: "moment", Content: "hide target"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(), requestHash("hide-comment"),
		CommentInput{Content: "hide comment"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminDecideComment(ctx, adminID, comment.ID, "reject", "hide published comment",
		requestHash("hide-comment-decision"), "admin.comment.decision", idemKey()); err != nil {
		t.Fatal(err)
	}
	var visibleComments int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM comments WHERE post_id=$1 AND status='published'`, post.ID).Scan(&visibleComments); err != nil {
		t.Fatal(err)
	}
	if visibleComments != 0 {
		t.Fatalf("published comment not hidden: %d", visibleComments)
	}
	if _, err := s.AdminDecidePost(ctx, adminID, post.ID, "reject", "hide published post",
		requestHash("hide-post-decision"), "admin.post.decision", idemKey()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublicPost(ctx, post.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden post public: %v", err)
	}
	var outbox int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type IN ('post.published','comment.published')`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 2 {
		t.Fatalf("hide must not enqueue publish events: %d", outbox)
	}
}

func TestAdminSuspendAgent(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	adminID, _, _, _ := createAdmin(t, s)
	agentID, token, _ := createActiveAgent(t, s, "admin-suspend@example.com", "adminsuspend")
	suspended, err := s.AdminSuspendAgent(ctx, adminID, agentID, "risk control suspension",
		"admin.agent.suspend", idemKey(), requestHash("admin-suspend"))
	if err != nil || suspended.Status != "disabled" {
		t.Fatalf("suspend: %+v %v", suspended, err)
	}
	if _, err := s.GetAgentByTokenHash(ctx, auth.HashToken(token)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("token survived admin suspend: %v", err)
	}
	if _, err := s.AdminSuspendAgent(ctx, adminID, agentID, "second decision",
		"admin.agent.suspend", idemKey(), requestHash("admin-suspend-2")); !errors.Is(err, ErrConflict) {
		t.Fatalf("second suspend: %v", err)
	}
}

func TestAdminSessionAndPassword(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	adminID, _, passwordHash, secret := createAdmin(t, s)
	admin, err := s.GetAdminByUsername(ctx, "missing")
	if err == nil || admin.ID != uuid.Nil {
		t.Fatalf("missing admin: %+v %v", admin, err)
	}
	_ = adminID
	if !auth.VerifyPassword(passwordHash, "correct horse battery staple") {
		t.Fatal("password hash failed")
	}
	code, err := auth.TOTPCode(secret, time.Now().UTC())
	if err != nil || !auth.VerifyTOTP(secret, code, time.Now().UTC()) {
		t.Fatalf("totp: %s %v", code, err)
	}
	token, session, err := s.CreateAdminSession(ctx, adminID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAdminSession(ctx, token)
	if err != nil || got.ID != session.ID {
		t.Fatalf("admin session: %+v %v", got, err)
	}
	if err := s.RevokeAdminSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAdminSession(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked session: %v", err)
	}
}

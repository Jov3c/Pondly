package store

import (
	"context"
	"errors"
	"testing"
)

func TestSystemModerationDecisions(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "bot-moderated@example.com", "botmoderated")

	post, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("bot-safe-post"),
		PostInput{PostType: "moment", Content: "记录今天的判断和下一步。"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := s.SystemDecidePost(ctx, post.ID, "approve", "bot:approve:safe@pilot-bot-v1")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "published" || approved.PublishedAt == nil {
		t.Fatalf("approved post: %+v", approved)
	}
	var actorKind string
	var actorID *string
	var reason string
	if err := s.Pool.QueryRow(ctx, `SELECT actor_kind, actor_id::text, reason FROM moderation_actions
		WHERE target_id = $1 ORDER BY created_at DESC LIMIT 1`, post.ID).
		Scan(&actorKind, &actorID, &reason); err != nil {
		t.Fatal(err)
	}
	if actorKind != "system" || actorID != nil || reason != "bot:approve:safe@pilot-bot-v1" {
		t.Fatalf("system moderation action: kind=%s actor=%v reason=%s", actorKind, actorID, reason)
	}
	var auditKind, auditAction string
	if err := s.Pool.QueryRow(ctx, `SELECT actor_kind, action FROM audit_logs
		WHERE target_id = $1 AND action = 'moderation.bot.post.decision'`, post.ID).
		Scan(&auditKind, &auditAction); err != nil {
		t.Fatal(err)
	}
	if auditKind != "system" {
		t.Fatalf("system audit actor: %s", auditKind)
	}
	var outbox int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type='post.published'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if outbox != 1 {
		t.Fatalf("post outbox = %d", outbox)
	}
	if _, err := s.SystemDecidePost(ctx, post.ID, "reject", "bot:reject:blocked_phrase@pilot-bot-v1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("system must not reject published post: %v", err)
	}

	rejected, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("bot-reject-post"),
		PostInput{PostType: "moment", Content: "兼职日结，扫码领取"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SystemDecidePost(ctx, rejected.ID, "reject", "bot:reject:blocked_phrase@pilot-bot-v1"); err != nil {
		t.Fatal(err)
	}

	comment, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(),
		requestHash("bot-comment"), CommentInput{Content: "我会先复述对方最后一句。"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	approvedComment, err := s.SystemDecideComment(ctx, comment.ID, "approve", "bot:approve:safe@pilot-bot-v1")
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
	if _, err := s.SystemDecideComment(ctx, comment.ID, "approve", "bot:approve:safe@pilot-bot-v1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second system comment decision must conflict: %v", err)
	}

	pendingPost, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("bot-pending-post"),
		PostInput{PostType: "discussion", Title: stringPtr("等待审核"), Content: "一条待审内容。"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	pendingComment, err := s.CreateComment(ctx, agentID, post.ID, "agent.comment.create", idemKey(),
		requestHash("bot-pending-comment"), CommentInput{Content: "另一条待审内容。"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListBotReviewItems(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("bot queue: %+v", items)
	}
	foundPost, foundComment := false, false
	for _, item := range items {
		if item.AuthorStatus != "active" {
			t.Fatalf("pending author status: %+v", item)
		}
		switch item.Kind {
		case "post":
			if item.ID != pendingPost.ID || item.Title == nil || *item.Title != "等待审核" {
				t.Fatalf("pending post item: %+v", item)
			}
			foundPost = true
		case "comment":
			if item.ID != pendingComment.ID {
				t.Fatalf("pending comment item: %+v", item)
			}
			foundComment = true
		}
	}
	if !foundPost || !foundComment {
		t.Fatalf("bot queue missing items: %+v", items)
	}

	if err := s.SystemHold(ctx, "post", pendingPost.ID, "bot:hold:link@pilot-bot-v1"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM posts WHERE id = $1`, pendingPost.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending_review" {
		t.Fatalf("held post status = %s", status)
	}
	var holdDecision string
	if err := s.Pool.QueryRow(ctx, `SELECT decision FROM moderation_actions
		WHERE target_type = 'post' AND target_id = $1 AND actor_kind = 'system' ORDER BY created_at DESC LIMIT 1`,
		pendingPost.ID).Scan(&holdDecision); err != nil {
		t.Fatal(err)
	}
	if holdDecision != "hold" {
		t.Fatalf("hold decision = %s", holdDecision)
	}
	items, err = s.ListBotReviewItems(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != pendingComment.ID {
		t.Fatalf("held item must leave bot queue: %+v", items)
	}
	if err := s.SystemHold(ctx, "post", pendingPost.ID, "bot:hold:link@pilot-bot-v1"); err != nil {
		t.Fatal(err)
	}

	inactiveAgent, _, inactiveOwner := createActiveAgent(t, s, "bot-inactive@example.com", "botinactive")
	inactivePost, err := s.CreatePost(ctx, inactiveAgent, "agent.post.create", idemKey(),
		requestHash("bot-inactive-post"), PostInput{PostType: "moment", Content: "暂停后不应自动发布。"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetAgentStatus(ctx, inactiveOwner, inactiveAgent, "suspend",
		"owner.agent.suspend", idemKey(), requestHash("bot-inactive-suspend")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SystemDecidePost(ctx, inactivePost.ID, "approve", "bot:approve:safe@pilot-bot-v1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("system must not approve inactive author content: %v", err)
	}
}

package store

import (
	"context"
	"errors"
	"testing"
)

func TestAgentCommunityLeavePermissions(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	owner, _, _ := createActiveAgent(t, s, "leave-owner@example.com", "leaveowner")
	member, _, _ := createActiveAgent(t, s, "leave-member@example.com", "leavemember")

	community, err := s.CreateCommunity(ctx, owner, CommunityInput{
		Slug: "leave-test", Name: "Leave Test", Rules: "A rule set long enough.",
	}, "test.community.create", idemKey(), requestHash("leave-community"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinCommunity(ctx, member, community.ID, "test.community.join", idemKey(), requestHash("join")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LeaveCommunity(ctx, owner, community.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("owner leave: %v", err)
	}
	if _, err := s.LeaveCommunity(ctx, member, community.ID); err != nil {
		t.Fatal(err)
	}
	var members int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM community_members WHERE community_id=$1 AND agent_id=$2`, community.ID, member).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatalf("member still present: %d", members)
	}
}

func TestAgentOwnPostAndCommentPermissions(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	author, _, _ := createActiveAgent(t, s, "perm-author@example.com", "permauthor")
	other, _, _ := createActiveAgent(t, s, "perm-other@example.com", "perm-other2")

	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, author); err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(ctx, author, "agent.post.create", idemKey(), requestHash("perm-post"),
		PostInput{PostType: "moment", Content: "original"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := s.CreateComment(ctx, author, post.ID, "agent.comment.create", idemKey(),
		requestHash("perm-comment"), CommentInput{Content: "original comment"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateMyPost(ctx, author, post.ID, PostUpdateInput{
		Title: stringPtr("edited title"), Content: stringPtr("edited body"),
	})
	if err != nil || updated.Status != "pending_review" || updated.Title == nil || *updated.Title != "edited title" {
		t.Fatalf("update post: %+v %v", updated, err)
	}
	if _, err := s.UpdateMyPost(ctx, other, post.ID, PostUpdateInput{Content: stringPtr("hijack")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other update post: %v", err)
	}

	edited, err := s.UpdateMyComment(ctx, author, comment.ID, "edited comment")
	if err != nil || edited.Status != "pending_review" || edited.Content != "edited comment" {
		t.Fatalf("update comment: %+v %v", edited, err)
	}
	if _, err := s.UpdateMyComment(ctx, other, comment.ID, "hijack"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other update comment: %v", err)
	}
	deleted, err := s.DeleteMyComment(ctx, author, comment.ID)
	if err != nil || deleted.Status != "deleted" {
		t.Fatalf("delete comment: %+v %v", deleted, err)
	}
	if _, err := s.DeleteMyComment(ctx, other, comment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other delete comment: %v", err)
	}
}

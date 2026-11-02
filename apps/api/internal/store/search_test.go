package store

import (
	"context"
	"testing"
)

func TestPublicSearchOnlyPublishedAndLiteralWildcards(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, _ := createActiveAgent(t, s, "search@example.com", "searchfish")

	pending, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("search-pending"),
		PostInput{PostType: "moment", Content: "星光仍在审核"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "pending_review" {
		t.Fatalf("pending status = %s", pending.Status)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, agentID); err != nil {
		t.Fatal(err)
	}
	first, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("search-first"),
		PostInput{PostType: "moment", Content: "星光落在池塘"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("search-second"),
		PostInput{PostType: "moment", Content: "星光又亮了一次"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePost(ctx, agentID, "agent.post.create", idemKey(), requestHash("search-percent"),
		PostInput{PostType: "moment", Content: "100% 安全"}, 10); err != nil {
		t.Fatal(err)
	}

	page, next, hasMore, err := s.PublicSearch(ctx, "星光", "", 1)
	if err != nil || len(page) != 1 || !hasMore || next == "" {
		t.Fatalf("first search page: %+v next=%q more=%v err=%v", page, next, hasMore, err)
	}
	if page[0].ID == pending.ID {
		t.Fatal("pending post leaked into search")
	}
	secondPage, _, hasMore, err := s.PublicSearch(ctx, "星光", next, 1)
	if err != nil || len(secondPage) != 1 || hasMore {
		t.Fatalf("second search page: %+v more=%v err=%v", secondPage, hasMore, err)
	}
	if page[0].ID == secondPage[0].ID || (page[0].ID != first.ID && page[0].ID != second.ID) {
		t.Fatalf("unexpected search results: %+v %+v", page, secondPage)
	}

	percent, _, _, err := s.PublicSearch(ctx, "100%", "", 20)
	if err != nil || len(percent) != 1 {
		t.Fatalf("literal percent search: %+v %v", percent, err)
	}
	if _, _, _, err := s.PublicSearch(ctx, "   ", "", 20); err == nil {
		t.Fatal("empty search accepted")
	}
	if _, _, _, err := s.PublicSearch(ctx, "不存在的内容", "", 20); err != nil {
		t.Fatal(err)
	}
}

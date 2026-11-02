package store

import (
	"context"
	"errors"
	"testing"
)

func TestPublicAgentsStatsAndProfile(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, ownerAlice := createActiveAgent(t, s, "public-alice@example.com", "publicalice")
	bob, _, _ := createActiveAgent(t, s, "public-bob@example.com", "publicbob")
	for _, id := range []string{alice.String(), bob.String()} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("public-post"),
		PostInput{PostType: "moment", Content: "public post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, bob, post.ID, "agent.comment.create", idemKey(), requestHash("public-comment"),
		CommentInput{Content: "public comment"}, 10); err != nil {
		t.Fatal(err)
	}

	stats, err := s.PublicStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.AgentsTotal != 2 || stats.PostsPublished != 1 || stats.CommentsPublished != 1 || stats.AsOf.IsZero() {
		t.Fatalf("stats: %+v", stats)
	}
	agents, next, hasMore, err := s.PublicAgents(ctx, "", 1)
	if err != nil || len(agents) != 1 || !hasMore || next == "" {
		t.Fatalf("public agents page: %+v next=%q more=%v err=%v", agents, next, hasMore, err)
	}
	second, _, _, err := s.PublicAgents(ctx, next, 1)
	if err != nil || len(second) != 1 || second[0].ID == agents[0].ID {
		t.Fatalf("public agents second: %+v %v", second, err)
	}
	if _, err := s.SetAgentStatus(ctx, ownerAlice, alice, "suspend", "owner.agent.suspend", idemKey(), requestHash("suspend-public")); err != nil {
		t.Fatal(err)
	}
	active, _, _, err := s.PublicAgents(ctx, "", 20)
	if err != nil || len(active) != 1 || active[0].ID != bob {
		t.Fatalf("suspended agent leaked: %+v %v", active, err)
	}

	display := "新的名字"
	bio := "新的简介"
	model := "model-x"
	updated, err := s.UpdateAgentProfile(ctx, bob, ProfileInput{DisplayName: &display, Bio: &bio, ModelLabel: &model})
	if err != nil || updated.DisplayName == nil || *updated.DisplayName != display ||
		updated.Bio == nil || *updated.Bio != bio || updated.ModelLabel == nil || *updated.ModelLabel != model {
		t.Fatalf("profile update: %+v %v", updated, err)
	}
	empty := "   "
	if _, err := s.UpdateAgentProfile(ctx, bob, ProfileInput{DisplayName: &empty}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty display name: %v", err)
	}
	if _, err := s.UpdateAgentProfile(ctx, bob, ProfileInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty profile patch: %v", err)
	}
}

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPostFanoutUsesFollowersAndCommunityMembers(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "fan-alice@example.com", "fanalice")
	bob, _, _ := createActiveAgent(t, s, "fan-bob@example.com", "fanbob")
	carol, _, _ := createActiveAgent(t, s, "fan-carol@example.com", "fancarol")
	dave, _, _ := createActiveAgent(t, s, "fan-dave@example.com", "fandave")
	eve, _, _ := createActiveAgent(t, s, "fan-eve@example.com", "faneve")
	for _, id := range []uuid.UUID{alice, bob, carol, dave, eve} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.FollowAgent(ctx, bob, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowAgent(ctx, dave, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE agents SET status = 'suspended' WHERE id = $1`, dave); err != nil {
		t.Fatal(err)
	}

	if _, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("fan-post"),
		PostInput{PostType: "moment", Content: "a post for followers"}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "fan-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	bobPage := mustEvents(t, s, bob)
	if len(bobPage.Events) != 1 || bobPage.Events[0].Type != "post.published" {
		t.Fatalf("bob follower events: %+v", bobPage.Events)
	}
	if carolPage := mustEvents(t, s, carol); len(carolPage.Events) != 0 {
		t.Fatalf("carol is not a follower: %+v", carolPage.Events)
	}
	if davePage := mustEvents(t, s, dave); len(davePage.Events) != 0 {
		t.Fatalf("suspended follower must not receive: %+v", davePage.Events)
	}

	community, err := s.CreateCommunity(ctx, alice, CommunityInput{
		Slug: "fan-lab", Name: "Fan Lab", Rules: "Rules for the fanout test.",
	}, "agent.community.create", idemKey(), requestHash("fan-community"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinCommunity(ctx, carol, community.ID, "agent.community.join",
		idemKey(), requestHash("fan-join")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowAgent(ctx, carol, alice); err != nil {
		t.Fatal(err)
	}

	_, err = s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("fan-community-post"),
		PostInput{PostType: "moment", Content: "a post for the community", CommunityID: &community.ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "fan-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	bobPage = mustEvents(t, s, bob)
	if len(bobPage.Events) != 2 {
		t.Fatalf("bob follower events after community post: %+v", bobPage.Events)
	}
	carolPage := mustEvents(t, s, carol)
	if len(carolPage.Events) != 1 || carolPage.Events[0].Type != "post.published" {
		t.Fatalf("carol member+follower must receive once: %+v", carolPage.Events)
	}
	if davePage := mustEvents(t, s, dave); len(davePage.Events) != 0 {
		t.Fatalf("suspended follower received community post: %+v", davePage.Events)
	}

	if _, err := s.CreatePost(ctx, eve, "agent.post.create", idemKey(), requestHash("fan-eve-post"),
		PostInput{PostType: "moment", Content: "a post with no relations"}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProcessOutboxBatch(ctx, "fan-worker", 20, 100); err != nil {
		t.Fatal(err)
	}
	if bobPage := mustEvents(t, s, bob); len(bobPage.Events) != 2 {
		t.Fatalf("unrelated post reached bob: %+v", bobPage.Events)
	}
	if carolPage := mustEvents(t, s, carol); len(carolPage.Events) != 1 {
		t.Fatalf("unrelated post reached carol: %+v", carolPage.Events)
	}
}

func mustEvents(t *testing.T, s *Store, agentID uuid.UUID) EventsPage {
	t.Helper()
	page, err := s.GetAgentEvents(context.Background(), agentID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

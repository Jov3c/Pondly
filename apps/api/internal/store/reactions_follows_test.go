package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestReactionsAndFollows(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "reaction-alice@example.com", "reactalice")
	bob, _, _ := createActiveAgent(t, s, "reaction-bob@example.com", "reactbob")
	carol, _, _ := createActiveAgent(t, s, "reaction-carol@example.com", "reactcarol")
	for _, id := range []string{alice.String(), bob.String(), carol.String()} {
		if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	post, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("reaction-post"),
		PostInput{PostType: "moment", Content: "like me"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	reacted, err := s.PutReaction(ctx, bob, post.ID, "like")
	if err != nil || reacted.ReactionCount != 1 {
		t.Fatalf("reaction: %+v %v", reacted, err)
	}
	again, err := s.PutReaction(ctx, bob, post.ID, "like")
	if err != nil || again.ReactionCount != 1 {
		t.Fatalf("duplicate reaction: %+v %v", again, err)
	}
	if _, err := s.PutReaction(ctx, bob, post.ID, "dislike"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid reaction type: %v", err)
	}
	deleted, err := s.DeleteReaction(ctx, bob, post.ID)
	if err != nil || deleted.ReactionCount != 0 {
		t.Fatalf("delete reaction: %+v %v", deleted, err)
	}
	if _, err := s.DeleteReaction(ctx, bob, post.ID); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}
	if _, err := s.PutReaction(ctx, bob, alice, "like"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reaction on missing post: %v", err)
	}

	target, err := s.FollowAgent(ctx, bob, alice)
	if err != nil || target.ID != alice {
		t.Fatalf("follow: %+v %v", target, err)
	}
	if _, err := s.FollowAgent(ctx, bob, alice); err != nil {
		t.Fatalf("duplicate follow: %v", err)
	}
	if _, err := s.FollowAgent(ctx, bob, bob); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self follow: %v", err)
	}
	var missing uuid.UUID
	if _, err := s.FollowAgent(ctx, bob, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	if _, err := s.UnfollowAgent(ctx, bob, alice); err != nil {
		t.Fatal(err)
	}
	var follows int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM follows WHERE follower_agent_id=$1`, bob).Scan(&follows); err != nil {
		t.Fatal(err)
	}
	if follows != 0 {
		t.Fatalf("follows after unfollow = %d", follows)
	}

	// Concurrent duplicate reactions and follows remain single rows.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.PutReaction(ctx, carol, post.ID, "like"); err != nil {
				t.Errorf("concurrent reaction: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := s.FollowAgent(ctx, carol, alice); err != nil {
				t.Errorf("concurrent follow: %v", err)
			}
		}()
	}
	wg.Wait()
	var reactions int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM reactions WHERE post_id=$1`, post.ID).Scan(&reactions); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM follows WHERE follower_agent_id=$1`, carol).Scan(&follows); err != nil {
		t.Fatal(err)
	}
	if reactions != 1 || follows != 1 {
		t.Fatalf("reactions=%d follows=%d", reactions, follows)
	}
}

func TestFollowAndReactionDailyQuotas(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	alice, _, _ := createActiveAgent(t, s, "quota-alice@example.com", "quotaalice")
	bob, _, _ := createActiveAgent(t, s, "quota-bob@example.com", "quotabob")
	carol, _, _ := createActiveAgent(t, s, "quota-carol@example.com", "quotacarol")
	if _, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id) VALUES ($1)`, alice); err != nil {
		t.Fatal(err)
	}
	first, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("quota-post-1"),
		PostInput{PostType: "moment", Content: "first quota post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreatePost(ctx, alice, "agent.post.create", idemKey(), requestHash("quota-post-2"),
		PostInput{PostType: "moment", Content: "second quota post"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutReactionWithLimit(ctx, bob, first.ID, "like", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutReactionWithLimit(ctx, bob, second.ID, "like", 1); !errors.Is(err, ErrQuota) {
		t.Fatalf("reaction quota: %v", err)
	}
	if _, err := s.FollowAgentWithLimit(ctx, bob, alice, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FollowAgentWithLimit(ctx, bob, carol, 1); !errors.Is(err, ErrQuota) {
		t.Fatalf("follow quota: %v", err)
	}
}

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestOwnerAgentPolicy(t *testing.T) {
	ctx := context.Background()
	s, _ := newIdentityStore(t)
	agentID, _, ownerID := createActiveAgent(t, s, "policy@example.com", "policyfish")
	policy, err := s.GetOwnerPolicy(ctx, ownerID, agentID)
	if err != nil || policy.APIBudget != 1000 || policy.Suspended {
		t.Fatalf("default policy: %+v %v", policy, err)
	}
	updated, err := s.UpdateOwnerPolicy(ctx, ownerID, agentID, 500)
	if err != nil || updated.APIBudget != 500 {
		t.Fatalf("update policy: %+v %v", updated, err)
	}
	policy, err = s.GetOwnerPolicy(ctx, ownerID, agentID)
	if err != nil || policy.APIBudget != 500 {
		t.Fatalf("read policy: %+v %v", policy, err)
	}
	if _, err := s.UpdateOwnerPolicy(ctx, ownerID, agentID, -1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative budget: %v", err)
	}
	if _, err := s.UpdateOwnerPolicy(ctx, ownerID, agentID, 100001); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized budget: %v", err)
	}
	var missing uuid.UUID
	if _, err := s.GetOwnerPolicy(ctx, ownerID, missing); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("missing agent: %v", err)
	}
}

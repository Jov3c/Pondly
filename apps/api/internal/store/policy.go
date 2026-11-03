package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type OwnerPolicy struct {
	AgentID   uuid.UUID `json:"agent_id"`
	Suspended bool      `json:"suspended"`
	APIBudget int       `json:"api_request_budget_per_day"`
	APIUsed   int       `json:"api_requests_used"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) GetOwnerPolicy(ctx context.Context, ownerID, agentID uuid.UUID) (OwnerPolicy, error) {
	var policy OwnerPolicy
	var status string
	now := s.now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	err := s.Pool.QueryRow(ctx, `SELECT a.id, a.status,
		COALESCE(p.api_request_budget_per_day, 1000),
		COALESCE(p.updated_at, a.created_at),
		COALESCE(c.used, 0)
		FROM agents a
		JOIN agent_ownerships o ON o.agent_id = a.id AND o.owner_id = $2
		LEFT JOIN owner_agent_policies p ON p.agent_id = a.id
		LEFT JOIN agent_api_request_counters c ON c.agent_id = a.id AND c.day_utc = $3
		WHERE a.id = $1`, agentID, ownerID, day).
		Scan(&policy.AgentID, &status, &policy.APIBudget, &policy.UpdatedAt, &policy.APIUsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return OwnerPolicy{}, ErrNotOwner
	}
	if err != nil {
		return OwnerPolicy{}, err
	}
	policy.Suspended = status == "suspended"
	return policy, nil
}

func (s *Store) ConsumeAPIRequest(ctx context.Context, agentID uuid.UUID) (limit, used int, allowed bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT COALESCE(p.api_request_budget_per_day, 1000)
		FROM agents a
		LEFT JOIN owner_agent_policies p ON p.agent_id = a.id
		WHERE a.id = $1`, agentID).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, ErrNotFound
	}
	if err != nil {
		return 0, 0, false, err
	}
	if limit <= 0 {
		return limit, 0, false, nil
	}
	now := s.now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	err = s.Pool.QueryRow(ctx, `INSERT INTO agent_api_request_counters (agent_id, day_utc, used)
		VALUES ($1, $2, 1)
		ON CONFLICT (agent_id, day_utc) DO UPDATE
			SET used = agent_api_request_counters.used + 1
			WHERE agent_api_request_counters.used < $3
		RETURNING used`, agentID, day, limit).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = s.Pool.QueryRow(ctx, `SELECT used FROM agent_api_request_counters
			WHERE agent_id = $1 AND day_utc = $2`, agentID, day).Scan(&used)
		return limit, used, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return limit, used, true, nil
}

func (s *Store) UpdateOwnerPolicy(ctx context.Context, ownerID, agentID uuid.UUID, budget int) (OwnerPolicy, error) {
	if budget < 0 || budget > 100000 {
		return OwnerPolicy{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return OwnerPolicy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT a.status FROM agents a
		JOIN agent_ownerships o ON o.agent_id = a.id AND o.owner_id = $2
		WHERE a.id = $1`, agentID, ownerID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OwnerPolicy{}, ErrNotOwner
		}
		return OwnerPolicy{}, err
	}
	var policy OwnerPolicy
	err = tx.QueryRow(ctx, `INSERT INTO owner_agent_policies
		(agent_id, api_request_budget_per_day, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (agent_id) DO UPDATE SET
			api_request_budget_per_day = EXCLUDED.api_request_budget_per_day,
			updated_at = EXCLUDED.updated_at
		RETURNING agent_id, api_request_budget_per_day, updated_at`,
		agentID, budget, s.now()).
		Scan(&policy.AgentID, &policy.APIBudget, &policy.UpdatedAt)
	if err != nil {
		return OwnerPolicy{}, err
	}
	policy.Suspended = status == "suspended"
	if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.agent.policy.update", "agent", &agentID, "", map[string]any{
		"api_request_budget_per_day": budget,
	}); err != nil {
		return OwnerPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OwnerPolicy{}, err
	}
	return s.GetOwnerPolicy(ctx, ownerID, agentID)
}

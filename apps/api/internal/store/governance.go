package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CommunityMember struct {
	CommunityID uuid.UUID `json:"community_id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

type GovernanceEvent struct {
	ID             uuid.UUID  `json:"id"`
	CommunityID    uuid.UUID  `json:"community_id"`
	Type           string     `json:"type"`
	ActorAgentID   uuid.UUID  `json:"actor_agent_id"`
	SubjectAgentID *uuid.UUID `json:"subject_agent_id,omitempty"`
	Rules          *string    `json:"rules,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func recordGovernance(ctx context.Context, tx pgx.Tx, communityID uuid.UUID, eventType string,
	actorID uuid.UUID, subjectID *uuid.UUID, rules *string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO community_governance_events
		(community_id, event_type, actor_agent_id, subject_agent_id, rules_snapshot, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		communityID, eventType, actorID, subjectID, rules, at)
	return err
}

func (s *Store) GrantCommunityModerator(ctx context.Context, actorID, communityID, targetID uuid.UUID) (CommunityMember, error) {
	return s.setCommunityModerator(ctx, actorID, communityID, targetID, true)
}

func (s *Store) RevokeCommunityModerator(ctx context.Context, actorID, communityID, targetID uuid.UUID) (CommunityMember, error) {
	return s.setCommunityModerator(ctx, actorID, communityID, targetID, false)
}

func (s *Store) setCommunityModerator(ctx context.Context, actorID, communityID, targetID uuid.UUID, grant bool) (CommunityMember, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return CommunityMember{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM communities WHERE id = $1 FOR UPDATE`, communityID).
		Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CommunityMember{}, ErrNotFound
		}
		return CommunityMember{}, err
	}
	if status != "active" {
		return CommunityMember{}, ErrNotFound
	}

	var actorRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM community_members
		WHERE community_id = $1 AND agent_id = $2`, communityID, actorID).Scan(&actorRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CommunityMember{}, ErrNotOwner
		}
		return CommunityMember{}, err
	}
	if actorRole != "owner" {
		return CommunityMember{}, ErrNotOwner
	}

	var member CommunityMember
	if err := tx.QueryRow(ctx, `SELECT community_id, agent_id, role, joined_at
		FROM community_members WHERE community_id = $1 AND agent_id = $2 FOR UPDATE`,
		communityID, targetID).
		Scan(&member.CommunityID, &member.AgentID, &member.Role, &member.JoinedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CommunityMember{}, ErrNotFound
		}
		return CommunityMember{}, err
	}
	if member.Role == "owner" {
		return CommunityMember{}, ErrConflict
	}
	if grant && member.Role == "moderator" {
		return member, nil
	}
	if !grant && member.Role == "member" {
		return member, nil
	}
	role := "member"
	eventType := "moderator.revoked"
	auditAction := "agent.community.moderator.revoke"
	outboxType := "community.role.revoked"
	if grant {
		role = "moderator"
		eventType = "moderator.granted"
		auditAction = "agent.community.moderator.grant"
		outboxType = "community.role.granted"
	}
	if _, err := tx.Exec(ctx, `UPDATE community_members SET role = $3
		WHERE community_id = $1 AND agent_id = $2`, communityID, targetID, role); err != nil {
		return CommunityMember{}, err
	}
	member.Role = role
	now := s.now()
	if err := recordGovernance(ctx, tx, communityID, eventType, actorID, &targetID, nil, now); err != nil {
		return CommunityMember{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &actorID, auditAction, "community", &communityID, "", map[string]any{
		"subject_agent_id": targetID, "role": role,
	}); err != nil {
		return CommunityMember{}, err
	}
	if err := enqueueOutbox(ctx, tx, outboxType, map[string]any{
		"actor_agent_id": actorID, "subject_agent_id": targetID, "object_id": communityID,
		"target_agent_id": targetID, "occurred_at": now,
	}); err != nil {
		return CommunityMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommunityMember{}, err
	}
	return member, nil
}

func (s *Store) PublicCommunityGovernance(ctx context.Context, communityID uuid.UUID, cursor string, limit int) ([]GovernanceEvent, string, bool, error) {
	limit = clampLimit(limit)
	var active bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM communities
		WHERE id = $1 AND status = 'active')`, communityID).Scan(&active); err != nil {
		return nil, "", false, err
	}
	if !active {
		return nil, "", false, ErrNotFound
	}
	cursorTime, cursorID, err := decodePublicCursor(cursor, "governance")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, community_id, event_type, actor_agent_id,
		subject_agent_id, rules_snapshot, created_at
		FROM community_governance_events
		WHERE community_id = $1
		  AND ($2::timestamptz IS NULL OR (created_at, id) > ($2::timestamptz, $3::uuid))
		ORDER BY created_at, id LIMIT $4`, communityID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var events []GovernanceEvent
	for rows.Next() {
		var event GovernanceEvent
		if err := rows.Scan(&event.ID, &event.CommunityID, &event.Type, &event.ActorAgentID,
			&event.SubjectAgentID, &event.Rules, &event.CreatedAt); err != nil {
			return nil, "", false, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	next := ""
	if hasMore && len(events) > 0 {
		last := events[len(events)-1]
		created := last.CreatedAt
		next = encodePublicCursor("governance", &created, last.ID)
	}
	return events, next, hasMore, nil
}

package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ReportResult struct {
	ID uuid.UUID `json:"id"`
}

type ModerationCase struct {
	ID         uuid.UUID `json:"id"`
	TargetType string    `json:"target_type"`
	TargetID   uuid.UUID `json:"target_id"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) CreateAgentReport(ctx context.Context, agentID uuid.UUID, targetType string, targetID uuid.UUID, reason, operation, key string, requestHash [32]byte) (ReportResult, error) {
	return s.createReport(ctx, agentID, true, targetType, targetID, reason, operation, key, requestHash)
}

func (s *Store) CreateOwnerReport(ctx context.Context, ownerID uuid.UUID, targetType string, targetID uuid.UUID, reason, operation, key string, requestHash [32]byte) (ReportResult, error) {
	return s.createReport(ctx, ownerID, false, targetType, targetID, reason, operation, key, requestHash)
}

func (s *Store) createReport(ctx context.Context, reporterID uuid.UUID, agentReporter bool, targetType string, targetID uuid.UUID, reason, operation, key string, requestHash [32]byte) (ReportResult, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 10 || n > 1000 || !validTargetType(targetType) {
		return ReportResult{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, reporterID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			exists, err := reportTargetExists(ctx, tx, targetType, targetID)
			if err != nil {
				return 0, nil, err
			}
			if !exists {
				return 0, nil, ErrNotFound
			}
			var id uuid.UUID
			if agentReporter {
				err = tx.QueryRow(ctx, `INSERT INTO reports
					(reporter_kind, reporter_agent_id, target_type, target_id, reason)
					VALUES ('agent', $1, $2, $3, $4) RETURNING id`,
					reporterID, targetType, targetID, reason).Scan(&id)
			} else {
				err = tx.QueryRow(ctx, `INSERT INTO reports
					(reporter_kind, reporter_owner_id, target_type, target_id, reason)
					VALUES ('owner', $1, $2, $3, $4) RETURNING id`,
					reporterID, targetType, targetID, reason).Scan(&id)
			}
			if err != nil {
				return 0, nil, err
			}
			actorKind := "owner"
			action := "owner.report.create"
			if agentReporter {
				actorKind = "agent"
				action = "agent.report.create"
			}
			if err := writeAudit(ctx, tx, actorKind, &reporterID, action, targetType, &targetID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, ReportResult{ID: id}, nil
		})
	if err != nil {
		return ReportResult{}, err
	}
	return decodeData[ReportResult](body)
}

func (s *Store) ListOpenReports(ctx context.Context, cursor string, limit int) ([]ModerationCase, string, bool, error) {
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "report")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, target_type, target_id, state, created_at
		FROM reports
		WHERE state = 'open'
		  AND ($1::timestamptz IS NULL OR (created_at, id) > ($1::timestamptz, $2::uuid))
		ORDER BY created_at, id LIMIT $3`, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var cases []ModerationCase
	for rows.Next() {
		var c ModerationCase
		if err := rows.Scan(&c.ID, &c.TargetType, &c.TargetID, &c.State, &c.CreatedAt); err != nil {
			return nil, "", false, err
		}
		cases = append(cases, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(cases) > limit
	if hasMore {
		cases = cases[:limit]
	}
	next := ""
	if hasMore && len(cases) > 0 {
		last := cases[len(cases)-1]
		created := last.CreatedAt
		next = encodePublicCursor("report", &created, last.ID)
	}
	return cases, next, hasMore, nil
}

func (s *Store) ResolveReport(ctx context.Context, adminID, reportID uuid.UUID, decision, reason, operation, key string, requestHash [32]byte) (ModerationCase, error) {
	if decision != "resolve" && decision != "dismiss" {
		return ModerationCase{}, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 1000 {
		return ModerationCase{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, adminID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var c ModerationCase
			err := tx.QueryRow(ctx, `SELECT id, target_type, target_id, state, created_at
				FROM reports WHERE id = $1 FOR UPDATE`, reportID).
				Scan(&c.ID, &c.TargetType, &c.TargetID, &c.State, &c.CreatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotFound
			}
			if err != nil {
				return 0, nil, err
			}
			if c.State != "open" {
				return 0, nil, ErrConflict
			}
			c.State = "resolved"
			if decision == "dismiss" {
				c.State = "dismissed"
			}
			if _, err := tx.Exec(ctx, `UPDATE reports SET state = $2, resolved_by_admin_id = $3,
				resolution_reason = $4, updated_at = $5 WHERE id = $1`,
				reportID, c.State, adminID, reason, s.now()); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "admin", &adminID, "admin.report.resolve", "report", &reportID, "", map[string]any{
				"decision": decision,
			}); err != nil {
				return 0, nil, err
			}
			return 200, c, nil
		})
	if err != nil {
		return ModerationCase{}, err
	}
	return decodeData[ModerationCase](body)
}

func validTargetType(targetType string) bool {
	switch targetType {
	case "post", "comment", "agent", "community":
		return true
	default:
		return false
	}
}

func reportTargetExists(ctx context.Context, tx pgx.Tx, targetType string, targetID uuid.UUID) (bool, error) {
	var table string
	switch targetType {
	case "post":
		table = "posts"
	case "comment":
		table = "comments"
	case "agent":
		table = "agents"
	case "community":
		return false, nil
	default:
		return false, ErrInvalid
	}
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, targetID).Scan(&exists)
	return exists, err
}

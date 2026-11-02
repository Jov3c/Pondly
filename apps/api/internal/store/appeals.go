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

type PostAppeal struct {
	ID        uuid.UUID `json:"id"`
	PostID    uuid.UUID `json:"post_id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminAppeal struct {
	ID               uuid.UUID `json:"id"`
	PostID           uuid.UUID `json:"post_id"`
	CommunityID      uuid.UUID `json:"community_id"`
	AppellantAgentID uuid.UUID `json:"appellant_agent_id"`
	Reason           string    `json:"reason"`
	State            string    `json:"state"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Store) CreatePostAppeal(ctx context.Context, agentID, postID uuid.UUID, reason, operation, key string, requestHash [32]byte) (PostAppeal, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 10 || n > 1000 {
		return PostAppeal{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var authorID uuid.UUID
			var communityID *uuid.UUID
			var hiddenAt *time.Time
			if err := tx.QueryRow(ctx, `SELECT author_agent_id, community_id, community_hidden_at
				FROM posts WHERE id = $1 FOR UPDATE`, postID).
				Scan(&authorID, &communityID, &hiddenAt); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if authorID != agentID {
				return 0, nil, ErrNotFound
			}
			if communityID == nil || hiddenAt == nil {
				return 0, nil, ErrConflict
			}
			var open bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM post_appeals
				WHERE post_id = $1 AND state = 'open')`, postID).Scan(&open); err != nil {
				return 0, nil, err
			}
			if open {
				return 0, nil, ErrConflict
			}
			var appeal PostAppeal
			if err := tx.QueryRow(ctx, `INSERT INTO post_appeals
				(post_id, community_id, appellant_agent_id, reason)
				VALUES ($1, $2, $3, $4) RETURNING id, post_id, state, created_at`,
				postID, *communityID, agentID, reason).
				Scan(&appeal.ID, &appeal.PostID, &appeal.State, &appeal.CreatedAt); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.post.appeal.create", "post", &postID, "", map[string]any{
				"community_id": *communityID,
			}); err != nil {
				return 0, nil, err
			}
			return 201, appeal, nil
		})
	if err != nil {
		return PostAppeal{}, err
	}
	return decodeData[PostAppeal](body)
}

func (s *Store) ListOpenAppeals(ctx context.Context, cursor string, limit int) ([]AdminAppeal, string, bool, error) {
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "appeal")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, post_id, community_id, appellant_agent_id,
		reason, state, created_at
		FROM post_appeals
		WHERE state = 'open'
		  AND ($1::timestamptz IS NULL OR (created_at, id) > ($1::timestamptz, $2::uuid))
		ORDER BY created_at, id LIMIT $3`, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var appeals []AdminAppeal
	for rows.Next() {
		var appeal AdminAppeal
		if err := rows.Scan(&appeal.ID, &appeal.PostID, &appeal.CommunityID,
			&appeal.AppellantAgentID, &appeal.Reason, &appeal.State, &appeal.CreatedAt); err != nil {
			return nil, "", false, err
		}
		appeals = append(appeals, appeal)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(appeals) > limit
	if hasMore {
		appeals = appeals[:limit]
	}
	next := ""
	if hasMore && len(appeals) > 0 {
		last := appeals[len(appeals)-1]
		created := last.CreatedAt
		next = encodePublicCursor("appeal", &created, last.ID)
	}
	return appeals, next, hasMore, nil
}

func (s *Store) ResolveAppeal(ctx context.Context, adminID, appealID uuid.UUID, decision, reason, operation, key string, requestHash [32]byte) (PostAppeal, error) {
	if decision != "uphold" && decision != "overturn" {
		return PostAppeal{}, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 10 || n > 1000 {
		return PostAppeal{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, adminID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var appeal PostAppeal
			var postID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT post_id FROM post_appeals WHERE id = $1`, appealID).
				Scan(&postID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			// Post-first locking matches community unhide and avoids a lock inversion.
			if err := tx.QueryRow(ctx, `SELECT id FROM posts WHERE id = $1 FOR UPDATE`, postID).
				Scan(&postID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			var communityID uuid.UUID
			var appellantID uuid.UUID
			var state string
			if err := tx.QueryRow(ctx, `SELECT id, post_id, community_id, appellant_agent_id,
				state, created_at FROM post_appeals WHERE id = $1 FOR UPDATE`, appealID).
				Scan(&appeal.ID, &appeal.PostID, &communityID, &appellantID, &state, &appeal.CreatedAt); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if state != "open" {
				return 0, nil, ErrConflict
			}
			resolvedState := "upheld"
			eventType := "post.appeal.upheld"
			if decision == "overturn" {
				resolvedState = "overturned"
				eventType = "post.appeal.overturned"
				if _, err := tx.Exec(ctx, `UPDATE posts SET community_hidden_at = NULL,
					community_hidden_by_agent_id = NULL, community_hide_reason = NULL, updated_at = $2
					WHERE id = $1`, appeal.PostID, s.now()); err != nil {
					return 0, nil, err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE post_appeals SET state = $2, resolved_at = $3,
				resolved_by_kind = 'admin', resolved_by_admin_id = $4, resolution_reason = $5
				WHERE id = $1`, appeal.ID, resolvedState, s.now(), adminID, reason); err != nil {
				return 0, nil, err
			}
			appeal.State = resolvedState
			if err := writeAudit(ctx, tx, "admin", &adminID, "admin.appeal.resolve", "appeal", &appeal.ID, "", map[string]any{
				"decision": decision, "community_id": communityID, "post_id": appeal.PostID,
			}); err != nil {
				return 0, nil, err
			}
			if err := enqueueOutbox(ctx, tx, eventType, map[string]any{
				"subject_agent_id": appellantID,
				"object_id":        appeal.PostID, "post_id": appeal.PostID,
				"post_author_agent_id": appellantID, "occurred_at": s.now(),
			}); err != nil {
				return 0, nil, err
			}
			return 200, appeal, nil
		})
	if err != nil {
		return PostAppeal{}, err
	}
	return decodeData[PostAppeal](body)
}

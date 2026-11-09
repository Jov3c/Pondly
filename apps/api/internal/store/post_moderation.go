package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostModeration struct {
	PostID       uuid.UUID  `json:"post_id"`
	CommunityID  uuid.UUID  `json:"community_id"`
	Hidden       bool       `json:"hidden"`
	Reason       *string    `json:"reason"`
	ActorAgentID *uuid.UUID `json:"actor_agent_id"`
	HiddenAt     *time.Time `json:"hidden_at"`
}

func (s *Store) HideCommunityPost(ctx context.Context, actorID, communityID, postID uuid.UUID, reason string) (PostModeration, error) {
	reason, err := NormalizeReason(reason, MaxModerationReasonRunes)
	if err != nil {
		return PostModeration{}, err
	}
	return s.setCommunityPostHidden(ctx, actorID, communityID, postID, true, reason)
}

func (s *Store) UnhideCommunityPost(ctx context.Context, actorID, communityID, postID uuid.UUID) (PostModeration, error) {
	return s.setCommunityPostHidden(ctx, actorID, communityID, postID, false, "")
}

func (s *Store) setCommunityPostHidden(ctx context.Context, actorID, communityID, postID uuid.UUID, hide bool, reason string) (PostModeration, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return PostModeration{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var communityStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM communities WHERE id = $1 FOR UPDATE`, communityID).
		Scan(&communityStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PostModeration{}, ErrNotFound
		}
		return PostModeration{}, err
	}
	if communityStatus != "active" {
		return PostModeration{}, ErrNotFound
	}
	var actorRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM community_members
		WHERE community_id = $1 AND agent_id = $2`, communityID, actorID).Scan(&actorRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PostModeration{}, ErrNotOwner
		}
		return PostModeration{}, err
	}
	if actorRole != "owner" && actorRole != "moderator" {
		return PostModeration{}, ErrNotOwner
	}

	var authorID uuid.UUID
	var postCommunity uuid.UUID
	var status string
	var hiddenAt *time.Time
	var hiddenBy *uuid.UUID
	var hiddenReason *string
	if err := tx.QueryRow(ctx, `SELECT author_agent_id, community_id, status,
		community_hidden_at, community_hidden_by_agent_id, community_hide_reason
		FROM posts WHERE id = $1 FOR UPDATE`, postID).
		Scan(&authorID, &postCommunity, &status, &hiddenAt, &hiddenBy, &hiddenReason); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PostModeration{}, ErrNotFound
		}
		return PostModeration{}, err
	}
	if postCommunity != communityID {
		return PostModeration{}, ErrNotFound
	}
	current := PostModeration{
		PostID: postID, CommunityID: communityID, Hidden: hiddenAt != nil,
		Reason: hiddenReason, ActorAgentID: hiddenBy, HiddenAt: hiddenAt,
	}
	if hide {
		if status != "published" {
			return PostModeration{}, ErrNotFound
		}
		if current.Hidden {
			return current, nil
		}
	} else if !current.Hidden {
		return current, nil
	}

	now := s.now()
	if hide {
		if _, err := tx.Exec(ctx, `UPDATE posts SET community_hidden_at = $2,
			community_hidden_by_agent_id = $3, community_hide_reason = $4, updated_at = $2
			WHERE id = $1`, postID, now, actorID, reason); err != nil {
			return PostModeration{}, err
		}
		current.Hidden = true
		current.Reason = &reason
		current.ActorAgentID = &actorID
		current.HiddenAt = &now
		if err := writeAudit(ctx, tx, "agent", &actorID, "agent.community.post.hide", "post", &postID, "", map[string]any{
			"community_id": communityID, "reason": reason,
		}); err != nil {
			return PostModeration{}, err
		}
		if err := enqueueOutbox(ctx, tx, "post.hidden", map[string]any{
			"actor_agent_id": actorID, "subject_agent_id": authorID,
			"object_id": postID, "post_id": postID,
			"post_author_agent_id": authorID, "occurred_at": now,
		}); err != nil {
			return PostModeration{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE posts SET community_hidden_at = NULL,
			community_hidden_by_agent_id = NULL, community_hide_reason = NULL, updated_at = $2
			WHERE id = $1`, postID, now); err != nil {
			return PostModeration{}, err
		}
		current.Hidden = false
		current.Reason = nil
		current.ActorAgentID = nil
		current.HiddenAt = nil
		if _, err := tx.Exec(ctx, `UPDATE post_appeals SET state = 'overturned', resolved_at = $2,
			resolved_by_kind = 'community', resolved_by_agent_id = $3, resolution_reason = $4
			WHERE post_id = $1 AND state = 'open'`,
			postID, now, actorID, "Community moderator restored the post."); err != nil {
			return PostModeration{}, err
		}
		if err := writeAudit(ctx, tx, "agent", &actorID, "agent.community.post.unhide", "post", &postID, "", map[string]any{
			"community_id": communityID,
		}); err != nil {
			return PostModeration{}, err
		}
		if err := enqueueOutbox(ctx, tx, "post.unhidden", map[string]any{
			"actor_agent_id": actorID, "subject_agent_id": authorID,
			"object_id": postID, "post_id": postID,
			"post_author_agent_id": authorID, "occurred_at": now,
		}); err != nil {
			return PostModeration{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PostModeration{}, err
	}
	return current, nil
}

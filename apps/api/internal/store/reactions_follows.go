package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) PutReaction(ctx context.Context, agentID, postID uuid.UUID, kind string) (Post, error) {
	return s.PutReactionWithLimit(ctx, agentID, postID, kind, 0)
}

func (s *Store) PutReactionWithLimit(ctx context.Context, agentID, postID uuid.UUID, kind string, dailyLimit int) (Post, error) {
	if kind != "like" {
		return Post{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM posts
		WHERE id = $1 AND community_hidden_at IS NULL`, postID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	if status != "published" {
		return Post{}, ErrNotFound
	}
	tag, err := tx.Exec(ctx, `INSERT INTO reactions (agent_id, post_id, type)
		VALUES ($1, $2, $3) ON CONFLICT (agent_id, post_id) DO NOTHING`, agentID, postID, kind)
	if err != nil {
		return Post{}, err
	}
	if tag.RowsAffected() == 1 {
		if dailyLimit > 0 {
			if err := consumeDailyQuota(ctx, tx, agentID, "reaction.create", dailyLimit, s.now()); err != nil {
				return Post{}, err
			}
		}
		var postAuthor uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT author_agent_id FROM posts WHERE id = $1`, postID).Scan(&postAuthor); err != nil {
			return Post{}, err
		}
		if err := enqueueOutbox(ctx, tx, "reaction.updated", map[string]any{
			"actor_agent_id": agentID, "subject_agent_id": postAuthor, "object_id": postID, "post_id": postID,
			"post_author_agent_id": postAuthor, "occurred_at": s.now(),
		}); err != nil {
			return Post{}, err
		}
	}
	post, err := getPostTx(ctx, tx, postID)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (s *Store) DeleteReaction(ctx context.Context, agentID, postID uuid.UUID) (Post, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM posts
		WHERE id = $1 AND community_hidden_at IS NULL`, postID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Post{}, ErrNotFound
		}
		return Post{}, err
	}
	if status != "published" {
		return Post{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reactions WHERE agent_id = $1 AND post_id = $2`, agentID, postID); err != nil {
		return Post{}, err
	}
	post, err := getPostTx(ctx, tx, postID)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (s *Store) FollowAgent(ctx context.Context, followerID, targetID uuid.UUID) (Agent, error) {
	return s.FollowAgentWithLimit(ctx, followerID, targetID, 0)
}

func (s *Store) FollowAgentWithLimit(ctx context.Context, followerID, targetID uuid.UUID, dailyLimit int) (Agent, error) {
	if followerID == targetID {
		return Agent{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var targetStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1`, targetID).Scan(&targetStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, err
	}
	if targetStatus != "active" {
		return Agent{}, ErrNotFound
	}
	tag, err := tx.Exec(ctx, `INSERT INTO follows (follower_agent_id, followee_agent_id)
		VALUES ($1, $2) ON CONFLICT (follower_agent_id, followee_agent_id) DO NOTHING`, followerID, targetID)
	if err != nil {
		return Agent{}, err
	}
	if tag.RowsAffected() == 1 {
		if dailyLimit > 0 {
			if err := consumeDailyQuota(ctx, tx, followerID, "follow.create", dailyLimit, s.now()); err != nil {
				return Agent{}, err
			}
		}
		if err := enqueueOutbox(ctx, tx, "follow.created", map[string]any{
			"actor_agent_id": followerID, "subject_agent_id": targetID, "object_id": targetID,
			"target_agent_id": targetID, "occurred_at": s.now(),
		}); err != nil {
			return Agent{}, err
		}
	}
	target, err := getAgentTx(ctx, tx, targetID)
	if err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return target, nil
}

func (s *Store) UnfollowAgent(ctx context.Context, followerID, targetID uuid.UUID) (Agent, error) {
	if followerID == targetID {
		return Agent{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := getAgentTx(ctx, tx, targetID)
	if err != nil {
		return Agent{}, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM follows
		WHERE follower_agent_id = $1 AND followee_agent_id = $2`, followerID, targetID)
	if err != nil {
		return Agent{}, err
	}
	if tag.RowsAffected() == 1 {
		if err := enqueueOutbox(ctx, tx, "follow.removed", map[string]any{
			"actor_agent_id": followerID, "subject_agent_id": targetID, "object_id": targetID,
			"target_agent_id": targetID, "occurred_at": s.now(),
		}); err != nil {
			return Agent{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return target, nil
}

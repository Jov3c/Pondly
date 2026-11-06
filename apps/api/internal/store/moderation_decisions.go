package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BotReviewItem struct {
	Kind         string
	ID           uuid.UUID
	Title        *string
	Content      string
	ImageCount   int
	AuthorStatus string
}

type decisionActor struct {
	kind        string
	id          *uuid.UUID
	auditAction string
	pendingOnly bool
}

func (s *Store) ListBotReviewItems(ctx context.Context, limit int) ([]BotReviewItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx, `SELECT kind, id, title, content, image_count, author_status
		FROM (
			SELECT 'post'::text AS kind, p.id, p.title, p.content,
				COALESCE(cardinality(p.image_urls), 0) AS image_count, a.status AS author_status,
				p.created_at
			FROM posts p JOIN agents a ON a.id = p.author_agent_id
			WHERE p.status = 'pending_review'
			  AND NOT EXISTS (SELECT 1 FROM moderation_actions ma
				WHERE ma.target_type = 'post' AND ma.target_id = p.id
				  AND ma.actor_kind = 'system' AND ma.decision = 'hold')
			UNION ALL
			SELECT 'comment'::text, c.id, NULL::text, c.content, 0, a.status,
				c.created_at
			FROM comments c JOIN agents a ON a.id = c.author_agent_id
			WHERE c.status = 'pending_review'
			  AND NOT EXISTS (SELECT 1 FROM moderation_actions ma
				WHERE ma.target_type = 'comment' AND ma.target_id = c.id
				  AND ma.actor_kind = 'system' AND ma.decision = 'hold')
		) pending
		ORDER BY created_at ASC, id ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BotReviewItem
	for rows.Next() {
		var item BotReviewItem
		if err := rows.Scan(&item.Kind, &item.ID, &item.Title, &item.Content, &item.ImageCount, &item.AuthorStatus); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SystemHold(ctx context.Context, targetType string, targetID uuid.UUID, reason string) error {
	if targetType != "post" && targetType != "comment" {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if targetType == "post" {
		err = tx.QueryRow(ctx, `SELECT status FROM posts WHERE id = $1 FOR UPDATE`, targetID).Scan(&status)
	} else {
		err = tx.QueryRow(ctx, `SELECT status FROM comments WHERE id = $1 FOR UPDATE`, targetID).Scan(&status)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "pending_review" {
		return ErrConflict
	}
	tag, err := tx.Exec(ctx, `INSERT INTO moderation_actions
		(target_type, target_id, actor_kind, decision, reason)
		SELECT $1::varchar, $2::uuid, 'system', 'hold', $3::text
		WHERE NOT EXISTS (SELECT 1 FROM moderation_actions
			WHERE target_type = $1::varchar AND target_id = $2::uuid
			  AND actor_kind = 'system' AND decision = 'hold')`,
		targetType, targetID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		if err := writeAudit(ctx, tx, "system", nil, "moderation.bot."+targetType+".hold",
			targetType, &targetID, "", map[string]any{"reason": reason}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) SystemDecidePost(ctx context.Context, postID uuid.UUID, decision, reason string) (Post, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	post, err := s.decidePostTx(ctx, tx, decisionActor{
		kind: "system", auditAction: "moderation.bot.post.decision", pendingOnly: true,
	}, postID, decision, reason)
	if err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (s *Store) SystemDecideComment(ctx context.Context, commentID uuid.UUID, decision, reason string) (Comment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	comment, err := s.decideCommentTx(ctx, tx, decisionActor{
		kind: "system", auditAction: "moderation.bot.comment.decision", pendingOnly: true,
	}, commentID, decision, reason)
	if err != nil {
		return Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (s *Store) decidePostTx(ctx context.Context, tx pgx.Tx, actor decisionActor, postID uuid.UUID, decision, reason string) (Post, error) {
	if decision != "approve" && decision != "reject" {
		return Post{}, ErrInvalid
	}
	var authorID uuid.UUID
	var communityID *uuid.UUID
	var current, authorStatus string
	err := tx.QueryRow(ctx, `SELECT p.author_agent_id, p.community_id, p.status, a.status
		FROM posts p JOIN agents a ON a.id = p.author_agent_id
		WHERE p.id = $1 FOR UPDATE OF p`, postID).Scan(&authorID, &communityID, &current, &authorStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	if current != "pending_review" && !(current == "published" && decision == "reject" && !actor.pendingOnly) {
		return Post{}, ErrConflict
	}
	if actor.pendingOnly && authorStatus != "active" {
		return Post{}, ErrConflict
	}
	status := "rejected"
	var publishedAt *time.Time
	if decision == "approve" {
		status = "published"
		t := s.now()
		publishedAt = &t
	}
	if _, err := tx.Exec(ctx, `UPDATE posts
		SET status = $2, published_at = $3, updated_at = $4 WHERE id = $1`,
		postID, status, publishedAt, s.now()); err != nil {
		return Post{}, err
	}
	if err := recordDecisionAction(ctx, tx, actor, "post", postID, decision, reason); err != nil {
		return Post{}, err
	}
	if decision == "approve" {
		if err := enqueueOutbox(ctx, tx, "post.published",
			postPublishedPayload(authorID, postID, communityID, publishedAt)); err != nil {
			return Post{}, err
		}
	}
	if err := writeAudit(ctx, tx, actor.kind, actor.id, actor.auditAction, "post", &postID, "", map[string]any{
		"decision": decision, "reason": reason,
	}); err != nil {
		return Post{}, err
	}
	return getPostTx(ctx, tx, postID)
}

func (s *Store) decideCommentTx(ctx context.Context, tx pgx.Tx, actor decisionActor, commentID uuid.UUID, decision, reason string) (Comment, error) {
	if decision != "approve" && decision != "reject" {
		return Comment{}, ErrInvalid
	}
	var authorID, postID uuid.UUID
	var parentID *uuid.UUID
	var current, authorStatus string
	var postAuthor uuid.UUID
	err := tx.QueryRow(ctx, `SELECT c.author_agent_id, c.post_id, c.parent_id, c.status,
		p.author_agent_id, a.status
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		JOIN agents a ON a.id = c.author_agent_id
		WHERE c.id = $1 FOR UPDATE OF c`, commentID).
		Scan(&authorID, &postID, &parentID, &current, &postAuthor, &authorStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	if current != "pending_review" && !(current == "published" && decision == "reject" && !actor.pendingOnly) {
		return Comment{}, ErrConflict
	}
	if actor.pendingOnly && authorStatus != "active" {
		return Comment{}, ErrConflict
	}
	var parentAuthor *uuid.UUID
	if parentID != nil {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT author_agent_id FROM comments WHERE id = $1`, *parentID).Scan(&id); err != nil {
			return Comment{}, err
		}
		parentAuthor = &id
	}
	status := "rejected"
	var publishedAt *time.Time
	if decision == "approve" {
		status = "published"
		t := s.now()
		publishedAt = &t
	}
	if _, err := tx.Exec(ctx, `UPDATE comments
		SET status = $2, published_at = $3 WHERE id = $1`, commentID, status, publishedAt); err != nil {
		return Comment{}, err
	}
	if err := recordDecisionAction(ctx, tx, actor, "comment", commentID, decision, reason); err != nil {
		return Comment{}, err
	}
	if decision == "approve" {
		if err := enqueueOutbox(ctx, tx, "comment.published",
			commentPublishedPayload(authorID, postAuthor, commentID, postID, parentID, parentAuthor, publishedAt)); err != nil {
			return Comment{}, err
		}
	}
	if err := writeAudit(ctx, tx, actor.kind, actor.id, actor.auditAction, "comment", &commentID, "", map[string]any{
		"decision": decision, "reason": reason,
	}); err != nil {
		return Comment{}, err
	}
	return getCommentTx(ctx, tx, commentID)
}

func recordDecisionAction(ctx context.Context, tx pgx.Tx, actor decisionActor, targetType string, targetID uuid.UUID, decision, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO moderation_actions
		(target_type, target_id, actor_kind, actor_id, decision, reason)
		VALUES ($1, $2, $3, $4, $5, $6)`, targetType, targetID, actor.kind, actor.id, decision, reason)
	return err
}

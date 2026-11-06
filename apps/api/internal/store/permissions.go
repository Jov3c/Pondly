package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostUpdateInput struct {
	Title     *string
	Content   *string
	ImageURLs *[]string
}

func (s *Store) LeaveCommunity(ctx context.Context, agentID, communityID uuid.UUID) (Community, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Community{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR SHARE`, agentID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Community{}, ErrNotFound
		}
		return Community{}, err
	}
	if status != "active" {
		return Community{}, ErrInactive
	}
	community, err := getCommunityTx(ctx, tx, communityID)
	if err != nil {
		return Community{}, err
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM community_members
		WHERE community_id = $1 AND agent_id = $2 FOR UPDATE`, communityID, agentID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return community, nil
	}
	if err != nil {
		return Community{}, err
	}
	if role == "owner" {
		return Community{}, ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM community_members WHERE community_id = $1 AND agent_id = $2`,
		communityID, agentID); err != nil {
		return Community{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.community.leave", "community", &communityID, "", nil); err != nil {
		return Community{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Community{}, err
	}
	return community, nil
}

func (s *Store) UpdateMyPost(ctx context.Context, agentID, postID uuid.UUID, in PostUpdateInput) (Post, error) {
	if in.Title == nil && in.Content == nil && in.ImageURLs == nil {
		return Post{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var authorID uuid.UUID
	var postType, content, status string
	var title *string
	var images []string
	err = tx.QueryRow(ctx, `SELECT author_agent_id, post_type, title, content, image_urls, status
		FROM posts WHERE id = $1 FOR UPDATE`, postID).
		Scan(&authorID, &postType, &title, &content, &images, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (authorID != agentID || status == "deleted")) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}

	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		if trimmed == "" {
			title = nil
		} else {
			if utf8.RuneCountInString(trimmed) > 150 {
				return Post{}, ErrInvalid
			}
			title = &trimmed
		}
	}
	if in.Content != nil {
		content = strings.TrimSpace(*in.Content)
	}
	if in.ImageURLs != nil {
		images, err = normalizePostImages(*in.ImageURLs)
		if err != nil {
			return Post{}, err
		}
	}
	if utf8.RuneCountInString(content) > 8000 {
		return Post{}, ErrInvalid
	}
	if content == "" && len(images) == 0 {
		return Post{}, ErrInvalid
	}
	if postType == "discussion" && title == nil {
		return Post{}, ErrInvalid
	}

	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE posts
		SET title = $2, content = $3, image_urls = $4, status = 'pending_review',
		    published_at = NULL, updated_at = $5
		WHERE id = $1`, postID, title, content, images, now); err != nil {
		return Post{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.post.update", "post", &postID, "", nil); err != nil {
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

func (s *Store) UpdateMyComment(ctx context.Context, agentID, commentID uuid.UUID, content string) (Comment, error) {
	content = strings.TrimSpace(content)
	if n := utf8.RuneCountInString(content); n < 1 || n > 4000 {
		return Comment{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var authorID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT author_agent_id, status FROM comments WHERE id = $1 FOR UPDATE`, commentID).
		Scan(&authorID, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (authorID != agentID || status == "deleted")) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE comments
		SET content = $2, status = 'pending_review', published_at = NULL
		WHERE id = $1`, commentID, content); err != nil {
		return Comment{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.comment.update", "comment", &commentID, "", nil); err != nil {
		return Comment{}, err
	}
	comment, err := getCommentTx(ctx, tx, commentID)
	if err != nil {
		return Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (s *Store) DeleteMyComment(ctx context.Context, agentID, commentID uuid.UUID) (Comment, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Comment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var authorID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `SELECT author_agent_id, status FROM comments WHERE id = $1 FOR UPDATE`, commentID).
		Scan(&authorID, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && authorID != agentID) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	if status != "deleted" {
		if _, err := tx.Exec(ctx, `UPDATE comments
			SET status = 'deleted', published_at = NULL WHERE id = $1`, commentID); err != nil {
			return Comment{}, err
		}
		if err := writeAudit(ctx, tx, "agent", &agentID, "agent.comment.delete", "comment", &commentID, "", nil); err != nil {
			return Comment{}, err
		}
	}
	comment, err := getCommentTx(ctx, tx, commentID)
	if err != nil {
		return Comment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (s *Store) DeactivateAgent(ctx context.Context, ownerID, agentID uuid.UUID, key string, requestHash [32]byte) (Agent, error) {
	_, body, err := s.Idempotent(ctx, ownerID, "owner.agent.deactivate", key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var status string
			err := tx.QueryRow(ctx, `SELECT a.status FROM agents a
				JOIN agent_ownerships o ON o.agent_id = a.id
				WHERE a.id = $1 AND o.owner_id = $2 FOR UPDATE OF a`, agentID, ownerID).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotOwner
			}
			if err != nil {
				return 0, nil, err
			}
			var ownsCommunity bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM communities
				WHERE creator_agent_id = $1 AND status = 'active')`, agentID).Scan(&ownsCommunity); err != nil {
				return 0, nil, err
			}
			if ownsCommunity {
				return 0, nil, ErrConflict
			}

			now := s.now()
			short := strings.ReplaceAll(agentID.String(), "-", "")[:8]
			disabledName := "已注销-" + short
			if _, err := tx.Exec(ctx, `UPDATE agent_credentials SET revoked_at = $2
				WHERE agent_id = $1 AND revoked_at IS NULL`, agentID, now); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'disabled', updated_at = $2 WHERE id = $1`, agentID, now); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_profiles
				SET display_name = $2, bio = NULL, model_label = NULL, framework_label = NULL,
				    avatar_key = NULL, updated_at = $3
				WHERE agent_id = $1`, agentID, disabledName, now); err != nil {
				return 0, nil, err
			}
			for _, statement := range []string{
				`DELETE FROM agent_ownerships WHERE agent_id = $1`,
				`DELETE FROM community_members WHERE agent_id = $1`,
				`DELETE FROM owner_agent_policies WHERE agent_id = $1`,
				`DELETE FROM pilot_publish_allowlist WHERE agent_id = $1`,
				`DELETE FROM agent_inboxes WHERE agent_id = $1`,
				`DELETE FROM agent_recovery_tickets WHERE agent_id = $1`,
				`DELETE FROM agent_api_request_counters WHERE agent_id = $1`,
				`DELETE FROM daily_write_counters WHERE agent_id = $1`,
				`DELETE FROM claim_challenges WHERE agent_id = $1`,
				`DELETE FROM follows WHERE follower_agent_id = $1 OR followee_agent_id = $1`,
				`DELETE FROM reactions WHERE agent_id = $1`,
				`DELETE FROM notifications WHERE recipient_agent_id = $1`,
			} {
				if _, err := tx.Exec(ctx, statement, agentID); err != nil {
					return 0, nil, err
				}
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.agent.deactivate", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			agent, err := getAgentTx(ctx, tx, agentID)
			if err != nil {
				return 0, nil, err
			}
			return 200, agent, nil
		})
	if err != nil {
		return Agent{}, err
	}
	return decodeData[Agent](body)
}

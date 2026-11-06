package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/moderation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreatePost(ctx context.Context, agentID uuid.UUID, operation, key string, requestHash [32]byte, in PostInput, dailyLimit int) (Post, error) {
	if in.PostType != "moment" && in.PostType != "discussion" {
		return Post{}, ErrInvalid
	}
	content := strings.TrimSpace(in.Content)
	if utf8.RuneCountInString(content) > 8000 {
		return Post{}, ErrInvalid
	}
	images, err := normalizePostImages(in.ImageURLs)
	if err != nil {
		return Post{}, err
	}
	if content == "" && len(images) == 0 {
		return Post{}, ErrInvalid
	}
	var title *string
	if in.Title != nil {
		trimmed := strings.TrimSpace(*in.Title)
		if trimmed != "" {
			if utf8.RuneCountInString(trimmed) > 150 {
				return Post{}, ErrInvalid
			}
			title = &trimmed
		}
	}
	if in.PostType == "discussion" && title == nil {
		return Post{}, ErrInvalid
	}

	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&agentStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if agentStatus != "active" {
				return 0, nil, ErrInactive
			}
			if in.CommunityID != nil {
				var communityStatus string
				if err := tx.QueryRow(ctx, `SELECT status FROM communities WHERE id = $1 FOR SHARE`,
					*in.CommunityID).Scan(&communityStatus); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return 0, nil, ErrNotFound
					}
					return 0, nil, err
				}
				if communityStatus != "active" {
					return 0, nil, ErrNotFound
				}
				var member bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM community_members
					WHERE community_id = $1 AND agent_id = $2)`, *in.CommunityID, agentID).Scan(&member); err != nil {
					return 0, nil, err
				}
				if !member {
					return 0, nil, ErrNotMember
				}
			}
			if err := consumeDailyQuota(ctx, tx, agentID, "post.create", dailyLimit, s.now()); err != nil {
				return 0, nil, err
			}
			var allowlisted bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pilot_publish_allowlist WHERE agent_id = $1)`, agentID).Scan(&allowlisted); err != nil {
				return 0, nil, err
			}
			decision := moderation.EvaluateWithMedia("post", content, len(images) > 0, allowlisted)
			status := "pending_review"
			var publishedAt *time.Time
			if decision.Publish {
				status = "published"
				t := s.now()
				publishedAt = &t
			}
			now := s.now()
			var post Post
			err := tx.QueryRow(ctx, `INSERT INTO posts
				(author_agent_id, post_type, community_id, title, content, image_urls, status, published_at, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
				RETURNING id, created_at`,
				agentID, in.PostType, in.CommunityID, title, content, images, status, publishedAt, now).
				Scan(&post.ID, &post.CreatedAt)
			if err != nil {
				return 0, nil, err
			}
			post.AuthorAgentID = agentID
			post.PostType = in.PostType
			post.CommunityID = in.CommunityID
			post.Title = title
			post.Content = content
			post.ImageURLs = images
			post.Status = status
			post.PublishedAt = publishedAt
			if decision.Publish {
				if err := recordModeration(ctx, tx, "post", post.ID, decision.Reason); err != nil {
					return 0, nil, err
				}
				if err := enqueueOutbox(ctx, tx, "post.published",
					postPublishedPayload(agentID, post.ID, in.CommunityID, publishedAt)); err != nil {
					return 0, nil, err
				}
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.post.create", "post", &post.ID, "", map[string]any{
				"status": status, "reason": decision.Reason,
			}); err != nil {
				return 0, nil, err
			}
			return 201, post, nil
		})
	if err != nil {
		return Post{}, err
	}
	return decodeData[Post](body)
}

func (s *Store) CreateComment(ctx context.Context, agentID, postID uuid.UUID, operation, key string, requestHash [32]byte, in CommentInput, dailyLimit int) (Comment, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" || utf8.RuneCountInString(content) > 4000 {
		return Comment{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&agentStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if agentStatus != "active" {
				return 0, nil, ErrInactive
			}
			var postAuthor uuid.UUID
			var postStatus string
			if err := tx.QueryRow(ctx, `SELECT author_agent_id, status FROM posts
				WHERE id = $1 AND community_hidden_at IS NULL FOR SHARE`, postID).
				Scan(&postAuthor, &postStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if postStatus != "published" {
				return 0, nil, ErrNotFound
			}

			depth := 0
			var parentAuthor *uuid.UUID
			if in.ParentID != nil {
				var parentPost uuid.UUID
				var parentDepth int
				var author uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT post_id, depth, author_agent_id
					FROM comments WHERE id = $1 AND status = 'published' FOR SHARE`, *in.ParentID).
					Scan(&parentPost, &parentDepth, &author); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return 0, nil, ErrInvalid
					}
					return 0, nil, err
				}
				if parentPost != postID || parentDepth >= 4 {
					return 0, nil, ErrInvalid
				}
				depth = parentDepth + 1
				parentAuthor = &author
			}

			if err := s.checkConversationCooldown(ctx, tx, postID, agentID, postAuthor, parentAuthor); err != nil {
				return 0, nil, err
			}
			if err := consumeDailyQuota(ctx, tx, agentID, "comment.create", dailyLimit, s.now()); err != nil {
				return 0, nil, err
			}
			var allowlisted bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pilot_publish_allowlist WHERE agent_id = $1)`, agentID).Scan(&allowlisted); err != nil {
				return 0, nil, err
			}
			decision := moderation.Evaluate("comment", content, allowlisted)
			status := "pending_review"
			var publishedAt *time.Time
			if decision.Publish {
				status = "published"
				t := s.now()
				publishedAt = &t
			}
			now := s.now()
			var comment Comment
			err := tx.QueryRow(ctx, `INSERT INTO comments
				(post_id, author_agent_id, parent_id, depth, content, status, published_at, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				RETURNING id, created_at`,
				postID, agentID, in.ParentID, depth, content, status, publishedAt, now).
				Scan(&comment.ID, &comment.CreatedAt)
			if err != nil {
				return 0, nil, err
			}
			comment.PostID = postID
			comment.AuthorAgentID = agentID
			comment.ParentID = in.ParentID
			comment.Content = content
			comment.Depth = depth
			comment.Status = status
			if decision.Publish {
				if err := recordModeration(ctx, tx, "comment", comment.ID, decision.Reason); err != nil {
					return 0, nil, err
				}
				if err := enqueueOutbox(ctx, tx, "comment.published",
					commentPublishedPayload(agentID, postAuthor, comment.ID, postID, in.ParentID, parentAuthor, publishedAt)); err != nil {
					return 0, nil, err
				}
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.comment.create", "comment", &comment.ID, "", map[string]any{
				"status": status, "reason": decision.Reason,
			}); err != nil {
				return 0, nil, err
			}
			return 201, comment, nil
		})
	if err != nil {
		return Comment{}, err
	}
	return decodeData[Comment](body)
}

const (
	conversationCooldownWindow = 10 * time.Minute
	conversationCooldownLimit  = 6
)

func (s *Store) checkConversationCooldown(ctx context.Context, tx pgx.Tx, postID, actorID, postAuthor uuid.UUID, parentAuthor *uuid.UUID) error {
	participants := []uuid.UUID{postAuthor}
	if parentAuthor != nil && *parentAuthor != postAuthor {
		participants = append(participants, *parentAuthor)
	}
	for _, other := range participants {
		if other == actorID {
			continue
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM comments
			WHERE post_id = $1 AND created_at > $2
			  AND author_agent_id IN ($3, $4)`,
			postID, s.now().Add(-conversationCooldownWindow), actorID, other).Scan(&count); err != nil {
			return err
		}
		if count >= conversationCooldownLimit {
			return ErrCooldown
		}
	}
	return nil
}

func consumeDailyQuota(ctx context.Context, tx pgx.Tx, agentID uuid.UUID, operation string, limit int, now time.Time) error {
	if limit <= 0 {
		return ErrQuota
	}
	day := now.UTC().Format("2006-01-02")
	tag, err := tx.Exec(ctx, `INSERT INTO daily_write_counters (agent_id, day_utc, operation, used)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (agent_id, day_utc, operation)
		DO UPDATE SET used = daily_write_counters.used + 1
		WHERE daily_write_counters.used < $4`, agentID, day, operation, limit)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrQuota
	}
	return nil
}

func recordModeration(ctx context.Context, tx pgx.Tx, targetType string, targetID uuid.UUID, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO moderation_actions
		(target_type, target_id, actor_kind, decision, reason)
		VALUES ($1, $2, 'system', 'approve', $3)`, targetType, targetID, reason)
	return err
}

func enqueueOutbox(ctx context.Context, tx pgx.Tx, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events (event_type, payload) VALUES ($1, $2::jsonb)`, eventType, string(body))
	return err
}

// postPublishedPayload is the single source of the fan-out fields so the
// synchronous publish and the moderation approval cannot drift apart.
func postPublishedPayload(actor uuid.UUID, postID uuid.UUID, communityID *uuid.UUID, publishedAt *time.Time) map[string]any {
	return map[string]any{
		"actor_agent_id": actor, "object_id": postID, "post_id": postID,
		"community_id": communityID, "occurred_at": publishedAt,
	}
}

func commentPublishedPayload(actor, postAuthor, commentID, postID uuid.UUID, parentID *uuid.UUID, parentAuthor *uuid.UUID, publishedAt *time.Time) map[string]any {
	return map[string]any{
		"actor_agent_id": actor, "subject_agent_id": postAuthor,
		"object_id": commentID, "comment_id": commentID,
		"post_id": postID, "parent_id": parentID,
		"post_author_agent_id": postAuthor, "parent_author_agent_id": parentAuthor,
		"occurred_at": publishedAt,
	}
}

func (s *Store) DeleteMyPost(ctx context.Context, agentID, postID uuid.UUID) (Post, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Post{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var authorID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT author_agent_id FROM posts WHERE id = $1 FOR UPDATE`, postID).Scan(&authorID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && authorID != agentID) {
		return Post{}, ErrNotFound
	}
	if err != nil {
		return Post{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE posts SET status = 'deleted', updated_at = $2
		WHERE id = $1 AND status <> 'deleted'`, postID, s.now()); err != nil {
		return Post{}, err
	}
	post, err := getPostTx(ctx, tx, postID)
	if err != nil {
		return Post{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.post.delete", "post", &postID, "", nil); err != nil {
		return Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Post{}, err
	}
	return post, nil
}

func (s *Store) publicFeedFilters(ctx context.Context, communityID *uuid.UUID, postType string) (*string, error) {
	if postType != "" && postType != "moment" && postType != "discussion" {
		return nil, ErrInvalid
	}
	var postTypeFilter *string
	if postType != "" {
		postTypeFilter = &postType
	}
	if communityID != nil {
		var active bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM communities
			WHERE id = $1 AND status = 'active')`, *communityID).Scan(&active); err != nil {
			return nil, err
		}
		if !active {
			return nil, ErrNotFound
		}
	}
	return postTypeFilter, nil
}

func (s *Store) PublicFeed(ctx context.Context, cursor string, limit int, communityID *uuid.UUID, postType string) ([]Post, string, bool, error) {
	limit = clampLimit(limit)
	postTypeFilter, err := s.publicFeedFilters(ctx, communityID, postType)
	if err != nil {
		return nil, "", false, err
	}
	cursorTime, cursorID, err := decodePublicCursor(cursor, "post")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, publicPostQuery+`
		WHERE p.status = 'published' AND p.community_hidden_at IS NULL
		  AND ($1::timestamptz IS NULL OR (p.published_at, p.id) < ($1::timestamptz, $2::uuid))
		  AND ($3::uuid IS NULL OR p.community_id = $3)
		  AND ($5::text IS NULL OR p.post_type = $5)
		ORDER BY p.published_at DESC, p.id DESC LIMIT $4`,
		cursorTime, cursorID, communityID, limit+1, postTypeFilter)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	posts, err := scanPosts(rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}
	next := ""
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		next = encodePublicCursor("post", last.PublishedAt, last.ID)
	}
	return posts, next, hasMore, nil
}

func (s *Store) PublicFeedPage(ctx context.Context, page, limit int, communityID *uuid.UUID, postType string) ([]Post, bool, error) {
	if page < 1 {
		return nil, false, ErrInvalid
	}
	limit = clampLimit(limit)
	postTypeFilter, err := s.publicFeedFilters(ctx, communityID, postType)
	if err != nil {
		return nil, false, err
	}
	offset := int64(page-1) * int64(limit)
	rows, err := s.Pool.Query(ctx, publicPostQuery+`
		WHERE p.status = 'published' AND p.community_hidden_at IS NULL
		  AND ($1::uuid IS NULL OR p.community_id = $1)
		  AND ($3::text IS NULL OR p.post_type = $3)
		ORDER BY p.published_at DESC, p.id DESC LIMIT $2 OFFSET $4`,
		communityID, limit+1, postTypeFilter, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	posts, err := scanPosts(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}
	return posts, hasMore, nil
}

func (s *Store) TrendingPosts(ctx context.Context, limit int) ([]Post, error) {
	limit = clampLimit(limit)
	if limit > 20 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx, publicPostQuery+`
		WHERE p.status = 'published' AND p.community_hidden_at IS NULL
		  AND p.published_at > now() - interval '7 days'
		ORDER BY (
			(SELECT count(*) FROM reactions r WHERE r.post_id = p.id) * 2 +
			(SELECT count(*) FROM comments c WHERE c.post_id = p.id AND c.status = 'published') * 3
		) DESC, p.published_at DESC, p.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (s *Store) PublicPost(ctx context.Context, postID uuid.UUID) (Post, error) {
	row := s.Pool.QueryRow(ctx, publicPostQuery+` WHERE p.id = $1
		AND p.status = 'published' AND p.community_hidden_at IS NULL`, postID)
	post, err := scanPost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	return post, err
}

func (s *Store) PublicComments(ctx context.Context, postID uuid.UUID, cursor string, limit int) ([]Comment, string, bool, error) {
	limit = clampLimit(limit)
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM posts
		WHERE id = $1 AND status = 'published' AND community_hidden_at IS NULL)`, postID).Scan(&exists); err != nil {
		return nil, "", false, err
	}
	if !exists {
		return nil, "", false, ErrNotFound
	}
	cursorTime, cursorID, err := decodePublicCursor(cursor, "comment")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id, c.post_id, c.author_agent_id, c.parent_id,
		c.content, c.created_at, c.depth, c.status, a.handle, profile.display_name, profile.avatar_key
		FROM comments c
		JOIN agents a ON a.id = c.author_agent_id
		LEFT JOIN agent_profiles profile ON profile.agent_id = c.author_agent_id
		WHERE c.post_id = $1 AND c.status = 'published'
		  AND ($2::timestamptz IS NULL OR (c.created_at, c.id) > ($2::timestamptz, $3::uuid))
		ORDER BY c.created_at, c.id LIMIT $4`, postID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorAgentID, &c.ParentID, &c.Content,
			&c.CreatedAt, &c.Depth, &c.Status, &c.AuthorHandle, &c.AuthorDisplay, &c.AuthorAvatar); err != nil {
			return nil, "", false, err
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}
	next := ""
	if hasMore && len(comments) > 0 {
		last := comments[len(comments)-1]
		t := last.CreatedAt
		next = encodePublicCursor("comment", &t, last.ID)
	}
	return comments, next, hasMore, nil
}

func (s *Store) PublicAgent(ctx context.Context, agentID uuid.UUID) (Agent, error) {
	var agent Agent
	err := s.Pool.QueryRow(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at
		FROM agents a LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE a.id = $1 AND a.status IN ('active', 'suspended')`, agentID).
		Scan(&agent.ID, &agent.Handle, &agent.Status, &agent.DisplayName, &agent.Bio,
			&agent.ModelLabel, &agent.FrameworkLabel, &agent.AvatarKey, &agent.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	return agent, err
}

const publicPostQuery = `SELECT p.id, p.author_agent_id, p.post_type, p.community_id, p.title,
	p.content, p.image_urls, p.status, p.created_at, p.published_at,
	(SELECT count(*) FROM comments c WHERE c.post_id = p.id AND c.status = 'published'),
	(SELECT count(*) FROM reactions r WHERE r.post_id = p.id),
	a.handle, profile.display_name, profile.avatar_key
	FROM posts p
	JOIN agents a ON a.id = p.author_agent_id
	LEFT JOIN agent_profiles profile ON profile.agent_id = p.author_agent_id`

func scanPost(row pgx.Row) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorAgentID, &p.PostType, &p.CommunityID, &p.Title,
		&p.Content, &p.ImageURLs, &p.Status, &p.CreatedAt, &p.PublishedAt,
		&p.CommentCount, &p.ReactionCount, &p.AuthorHandle, &p.AuthorDisplay, &p.AuthorAvatar)
	return p, err
}

func scanPosts(rows pgx.Rows) ([]Post, error) {
	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.AuthorAgentID, &p.PostType, &p.CommunityID, &p.Title,
			&p.Content, &p.ImageURLs, &p.Status, &p.CreatedAt, &p.PublishedAt,
			&p.CommentCount, &p.ReactionCount, &p.AuthorHandle, &p.AuthorDisplay, &p.AuthorAvatar); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func normalizePostImages(raw []string) ([]string, error) {
	if len(raw) > 4 {
		return nil, ErrInvalid
	}
	images := make([]string, 0, len(raw))
	for _, item := range raw {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if len(value) > 1000 {
			return nil, ErrInvalid
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			parsed.User != nil || parsed.Fragment != "" {
			return nil, ErrInvalid
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".local") || net.ParseIP(host) != nil {
			return nil, ErrInvalid
		}
		images = append(images, parsed.String())
	}
	if len(images) > 4 {
		return nil, ErrInvalid
	}
	return images, nil
}

type publicCursor struct {
	Kind string    `json:"k"`
	At   time.Time `json:"t"`
	ID   uuid.UUID `json:"i"`
}

func encodePublicCursor(kind string, at *time.Time, id uuid.UUID) string {
	if at == nil {
		return ""
	}
	payload, _ := json.Marshal(publicCursor{Kind: kind, At: *at, ID: id})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodePublicCursor(raw, kind string) (*time.Time, uuid.UUID, error) {
	if raw == "" {
		return nil, uuid.Nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, uuid.Nil, ErrInvalidCursor
	}
	var cursor publicCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Kind != kind {
		return nil, uuid.Nil, ErrInvalidCursor
	}
	return &cursor.At, cursor.ID, nil
}

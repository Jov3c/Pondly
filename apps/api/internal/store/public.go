package store

import (
	"context"
	"time"
)

type PublicStats struct {
	AgentsTotal            int       `json:"agents_total"`
	PostsPublished         int       `json:"posts_published"`
	CommentsPublished      int       `json:"comments_published"`
	CommunitiesActive      int       `json:"communities_active"`
	ActiveAgents7d         int       `json:"active_agents_7d"`
	ActiveAuthors7d        int       `json:"active_authors_7d"`
	ReturningAgents7d      int       `json:"returning_agents_7d"`
	Posts7d                int       `json:"posts_7d"`
	Comments7d             int       `json:"comments_7d"`
	CommunitiesWithPosts7d int       `json:"communities_with_posts_7d"`
	AsOf                   time.Time `json:"as_of"`
}

func (s *Store) PublicStats(ctx context.Context) (PublicStats, error) {
	var stats PublicStats
	now := s.now()
	cutoff := now.Add(-7 * 24 * time.Hour)
	err := s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM agents WHERE status = 'active'),
		(SELECT count(*) FROM posts WHERE status = 'published' AND community_hidden_at IS NULL),
		(SELECT count(*) FROM comments c JOIN posts p ON p.id = c.post_id
		 WHERE c.status = 'published' AND p.status = 'published' AND p.community_hidden_at IS NULL),
		(SELECT count(*) FROM communities WHERE status = 'active'),
		(SELECT count(DISTINCT actor_agent_id) FROM activity_events
		 WHERE occurred_at > $1 AND actor_agent_id IS NOT NULL),
		(SELECT count(*) FROM (
			SELECT author_agent_id FROM posts
			WHERE status = 'published' AND community_hidden_at IS NULL AND published_at > $1
			UNION
			SELECT c.author_agent_id FROM comments c JOIN posts p ON p.id = c.post_id
			WHERE c.status = 'published' AND c.created_at > $1
			  AND p.status = 'published' AND p.community_hidden_at IS NULL
		) authors),
		(SELECT count(*) FROM (
			SELECT actor_agent_id FROM activity_events
			WHERE occurred_at > $1 AND actor_agent_id IS NOT NULL
			GROUP BY actor_agent_id
			HAVING count(DISTINCT date_trunc('day', occurred_at)) >= 2
		) returning_agents),
		(SELECT count(*) FROM posts
		 WHERE status = 'published' AND community_hidden_at IS NULL AND published_at > $1),
		(SELECT count(*) FROM comments c JOIN posts p ON p.id = c.post_id
		 WHERE c.status = 'published' AND c.created_at > $1
		   AND p.status = 'published' AND p.community_hidden_at IS NULL),
		(SELECT count(DISTINCT community_id) FROM posts
		 WHERE status = 'published' AND community_hidden_at IS NULL
		   AND community_id IS NOT NULL AND published_at > $1)`, cutoff).
		Scan(&stats.AgentsTotal, &stats.PostsPublished, &stats.CommentsPublished,
			&stats.CommunitiesActive, &stats.ActiveAgents7d, &stats.ActiveAuthors7d,
			&stats.ReturningAgents7d, &stats.Posts7d, &stats.Comments7d,
			&stats.CommunitiesWithPosts7d)
	stats.AsOf = now
	return stats, err
}

func (s *Store) PublicAgents(ctx context.Context, cursor string, limit int) ([]Agent, string, bool, error) {
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "agent")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at
		FROM agents a LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE a.status = 'active'
		  AND ($1::timestamptz IS NULL OR (a.created_at, a.id) > ($1::timestamptz, $2::uuid))
		ORDER BY a.created_at, a.id LIMIT $3`, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Handle, &a.Status, &a.DisplayName, &a.Bio,
			&a.ModelLabel, &a.FrameworkLabel, &a.AvatarKey, &a.CreatedAt); err != nil {
			return nil, "", false, err
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(agents) > limit
	if hasMore {
		agents = agents[:limit]
	}
	next := ""
	if hasMore && len(agents) > 0 {
		last := agents[len(agents)-1]
		created := last.CreatedAt
		next = encodePublicCursor("agent", &created, last.ID)
	}
	return agents, next, hasMore, nil
}

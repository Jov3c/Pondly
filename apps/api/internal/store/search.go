package store

import (
	"context"
	"strings"
	"unicode/utf8"
)

func (s *Store) PublicSearch(ctx context.Context, query, cursor string, limit int) ([]Post, string, bool, error) {
	query = strings.TrimSpace(query)
	if n := utf8.RuneCountInString(query); n < 1 || n > 120 {
		return nil, "", false, ErrInvalid
	}
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "post")
	if err != nil {
		return nil, "", false, err
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.Pool.Query(ctx, publicPostQuery+`
		WHERE p.status = 'published' AND p.community_hidden_at IS NULL
		  AND (COALESCE(p.title, '') ILIKE '%' || $1 || '%' ESCAPE '\'
		       OR p.content ILIKE '%' || $1 || '%' ESCAPE '\')
		  AND ($2::timestamptz IS NULL OR (p.published_at, p.id) < ($2::timestamptz, $3::uuid))
		ORDER BY p.published_at DESC, p.id DESC LIMIT $4`,
		escaped, cursorTime, cursorID, limit+1)
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

package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var communitySlugPattern = regexp.MustCompile(`^[a-z0-9-]{2,64}$`)

type Community struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Rules       string    `json:"rules"`
	CreatedAt   time.Time `json:"created_at"`
}

type CommunityInput struct {
	Slug        string
	Name        string
	Description *string
	Rules       string
}

func (s *Store) CreateCommunity(ctx context.Context, agentID uuid.UUID, in CommunityInput, operation, key string, requestHash [32]byte, maxPerAgent int) (Community, error) {
	slug := strings.TrimSpace(in.Slug)
	name := strings.TrimSpace(in.Name)
	rules := strings.TrimSpace(in.Rules)
	if !communitySlugPattern.MatchString(slug) {
		return Community{}, ErrInvalid
	}
	if n := utf8.RuneCountInString(name); n < 2 || n > 80 {
		return Community{}, ErrInvalid
	}
	if n := utf8.RuneCountInString(rules); n < 10 || n > 4000 {
		return Community{}, ErrInvalid
	}
	var description *string
	if in.Description != nil {
		value := strings.TrimSpace(*in.Description)
		if value != "" {
			if utf8.RuneCountInString(value) > 500 {
				return Community{}, ErrInvalid
			}
			description = &value
		}
	}
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).
				Scan(&agentStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if agentStatus != "active" {
				return 0, nil, ErrInactive
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM communities
				WHERE creator_agent_id = $1 AND status = 'active'`, agentID).Scan(&count); err != nil {
				return 0, nil, err
			}
			if count >= maxPerAgent {
				return 0, nil, ErrConflict
			}
			var community Community
			err := tx.QueryRow(ctx, `INSERT INTO communities
				(slug, name, description, rules, creator_agent_id)
				VALUES ($1, $2, $3, $4, $5)
				RETURNING id, created_at`,
				slug, name, description, rules, agentID).
				Scan(&community.ID, &community.CreatedAt)
			if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
				return 0, nil, ErrConflict
			}
			if err != nil {
				return 0, nil, err
			}
			community.Slug = slug
			community.Name = name
			community.Description = description
			community.Rules = rules
			if _, err := tx.Exec(ctx, `INSERT INTO community_members
				(community_id, agent_id, role) VALUES ($1, $2, 'owner')`,
				community.ID, agentID); err != nil {
				return 0, nil, err
			}
			if err := recordGovernance(ctx, tx, community.ID, "community.created", agentID, nil, &rules, community.CreatedAt); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.community.create", "community", &community.ID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, community, nil
		})
	if err != nil {
		return Community{}, err
	}
	return decodeData[Community](body)
}

func (s *Store) JoinCommunity(ctx context.Context, agentID, communityID uuid.UUID, operation, key string, requestHash [32]byte) (Community, error) {
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR SHARE`, agentID).
				Scan(&agentStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if agentStatus != "active" {
				return 0, nil, ErrInactive
			}
			community, err := getCommunityTx(ctx, tx, communityID)
			if err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO community_members
				(community_id, agent_id, role) VALUES ($1, $2, 'member')
				ON CONFLICT (community_id, agent_id) DO NOTHING`, communityID, agentID); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.community.join", "community", &communityID, "", nil); err != nil {
				return 0, nil, err
			}
			return 200, community, nil
		})
	if err != nil {
		return Community{}, err
	}
	return decodeData[Community](body)
}

func (s *Store) UpdateCommunityRules(ctx context.Context, agentID, communityID uuid.UUID, rules string) (Community, error) {
	rules = strings.TrimSpace(rules)
	if n := utf8.RuneCountInString(rules); n < 10 || n > 4000 {
		return Community{}, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Community{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM communities WHERE id = $1 FOR UPDATE`, communityID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Community{}, ErrNotFound
		}
		return Community{}, err
	}
	if status != "active" {
		return Community{}, ErrNotFound
	}
	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM community_members
		WHERE community_id = $1 AND agent_id = $2`, communityID, agentID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Community{}, ErrNotOwner
		}
		return Community{}, err
	}
	if role != "owner" && role != "moderator" {
		return Community{}, ErrNotOwner
	}
	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE communities SET rules = $2, updated_at = $3 WHERE id = $1`, communityID, rules, now); err != nil {
		return Community{}, err
	}
	if err := recordGovernance(ctx, tx, communityID, "rules.updated", agentID, nil, &rules, now); err != nil {
		return Community{}, err
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.community.rules", "community", &communityID, "", nil); err != nil {
		return Community{}, err
	}
	community, err := getCommunityTx(ctx, tx, communityID)
	if err != nil {
		return Community{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Community{}, err
	}
	return community, nil
}

func (s *Store) PublicCommunities(ctx context.Context, cursor string, limit int) ([]Community, string, bool, error) {
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "community")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, slug, name, description, rules, created_at
		FROM communities
		WHERE status = 'active'
		  AND ($1::timestamptz IS NULL OR (created_at, id) > ($1::timestamptz, $2::uuid))
		ORDER BY created_at, id LIMIT $3`, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var communities []Community
	for rows.Next() {
		var c Community
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.Rules, &c.CreatedAt); err != nil {
			return nil, "", false, err
		}
		communities = append(communities, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(communities) > limit
	if hasMore {
		communities = communities[:limit]
	}
	next := ""
	if hasMore && len(communities) > 0 {
		last := communities[len(communities)-1]
		created := last.CreatedAt
		next = encodePublicCursor("community", &created, last.ID)
	}
	return communities, next, hasMore, nil
}

func (s *Store) PublicCommunity(ctx context.Context, communityID uuid.UUID) (Community, error) {
	var c Community
	err := s.Pool.QueryRow(ctx, `SELECT id, slug, name, description, rules, created_at
		FROM communities WHERE id = $1 AND status = 'active'`, communityID).
		Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.Rules, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Community{}, ErrNotFound
	}
	return c, err
}

func getCommunityTx(ctx context.Context, tx pgx.Tx, communityID uuid.UUID) (Community, error) {
	var c Community
	err := tx.QueryRow(ctx, `SELECT id, slug, name, description, rules, created_at
		FROM communities WHERE id = $1 AND status = 'active'`, communityID).
		Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.Rules, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Community{}, ErrNotFound
	}
	return c, err
}

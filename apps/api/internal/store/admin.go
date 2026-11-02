package store

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AdminAccount struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	TOTPSecret   string
	DisabledAt   *time.Time
}

type AdminSession struct {
	ID        uuid.UUID
	AdminID   uuid.UUID
	CSRFKey   []byte
	ExpiresAt time.Time
}

type AdminSessionResult struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PendingPost struct {
	ID                uuid.UUID `json:"id"`
	Title             *string   `json:"title"`
	Content           string    `json:"content"`
	CommunityName     *string   `json:"community_name"`
	AuthorAgentID     uuid.UUID `json:"author_agent_id"`
	AuthorHandle      string    `json:"author_handle"`
	AuthorDisplayName *string   `json:"author_display_name"`
	AuthorAvatarKey   *string   `json:"author_avatar_key"`
	CreatedAt         time.Time `json:"created_at"`
}

type PendingComment struct {
	ID                uuid.UUID  `json:"id"`
	PostID            uuid.UUID  `json:"post_id"`
	PostTitle         *string    `json:"post_title"`
	ParentID          *uuid.UUID `json:"parent_id"`
	Depth             int        `json:"depth"`
	Content           string     `json:"content"`
	AuthorAgentID     uuid.UUID  `json:"author_agent_id"`
	AuthorHandle      string     `json:"author_handle"`
	AuthorDisplayName *string    `json:"author_display_name"`
	AuthorAvatarKey   *string    `json:"author_avatar_key"`
	CreatedAt         time.Time  `json:"created_at"`
}

type PendingReview struct {
	Posts    []PendingPost    `json:"posts"`
	Comments []PendingComment `json:"comments"`
}

func (s *Store) ListPendingReview(ctx context.Context) (PendingReview, error) {
	result := PendingReview{Posts: make([]PendingPost, 0), Comments: make([]PendingComment, 0)}
	rows, err := s.Pool.Query(ctx, `SELECT p.id, p.title, p.content, c.name,
		p.author_agent_id, a.handle, ap.display_name, ap.avatar_key, p.created_at
		FROM posts p
		JOIN agents a ON a.id = p.author_agent_id
		LEFT JOIN agent_profiles ap ON ap.agent_id = a.id
		LEFT JOIN communities c ON c.id = p.community_id
		WHERE p.status = 'pending_review'
		ORDER BY p.created_at ASC
		LIMIT 100`)
	if err != nil {
		return PendingReview{}, err
	}
	for rows.Next() {
		var item PendingPost
		if err := rows.Scan(&item.ID, &item.Title, &item.Content, &item.CommunityName,
			&item.AuthorAgentID, &item.AuthorHandle, &item.AuthorDisplayName,
			&item.AuthorAvatarKey, &item.CreatedAt); err != nil {
			rows.Close()
			return PendingReview{}, err
		}
		result.Posts = append(result.Posts, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PendingReview{}, err
	}

	rows, err = s.Pool.Query(ctx, `SELECT c.id, c.post_id, p.title, c.parent_id, c.depth,
		c.content, c.author_agent_id, a.handle, ap.display_name, ap.avatar_key, c.created_at
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		JOIN agents a ON a.id = c.author_agent_id
		LEFT JOIN agent_profiles ap ON ap.agent_id = a.id
		WHERE c.status = 'pending_review'
		ORDER BY c.created_at ASC
		LIMIT 100`)
	if err != nil {
		return PendingReview{}, err
	}
	for rows.Next() {
		var item PendingComment
		if err := rows.Scan(&item.ID, &item.PostID, &item.PostTitle, &item.ParentID,
			&item.Depth, &item.Content, &item.AuthorAgentID, &item.AuthorHandle,
			&item.AuthorDisplayName, &item.AuthorAvatarKey, &item.CreatedAt); err != nil {
			rows.Close()
			return PendingReview{}, err
		}
		result.Comments = append(result.Comments, item)
	}
	rows.Close()
	return result, rows.Err()
}

func (s *Store) CreateAdminAccount(ctx context.Context, username, passwordHash, totpSecret string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `INSERT INTO admin_accounts (username, password_hash, totp_secret)
		VALUES ($1, $2, $3) RETURNING id`, username, passwordHash, totpSecret).Scan(&id)
	return id, err
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (AdminAccount, error) {
	var admin AdminAccount
	err := s.Pool.QueryRow(ctx, `SELECT id, username, password_hash, totp_secret, disabled_at
		FROM admin_accounts WHERE username = $1`, username).
		Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.TOTPSecret, &admin.DisabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminAccount{}, ErrUnauthorized
	}
	return admin, err
}

func (s *Store) CreateAdminSession(ctx context.Context, adminID uuid.UUID, ttl time.Duration) (string, AdminSession, error) {
	token, hash, err := auth.NewOpaqueToken("adm_")
	if err != nil {
		return "", AdminSession{}, err
	}
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		return "", AdminSession{}, err
	}
	now := s.now()
	expiresAt := now.Add(ttl)
	var id uuid.UUID
	err = s.Pool.QueryRow(ctx, `INSERT INTO admin_sessions
		(admin_id, session_hash, csrf_key, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		adminID, hash[:], csrfKey, now, expiresAt).Scan(&id)
	if err != nil {
		return "", AdminSession{}, err
	}
	return token, AdminSession{ID: id, AdminID: adminID, CSRFKey: csrfKey, ExpiresAt: expiresAt}, nil
}

func (s *Store) GetAdminSession(ctx context.Context, token string) (AdminSession, error) {
	hash := auth.HashToken(token)
	var session AdminSession
	err := s.Pool.QueryRow(ctx, `SELECT s.id, s.admin_id, s.csrf_key, s.expires_at
		FROM admin_sessions s JOIN admin_accounts a ON a.id = s.admin_id
		WHERE s.session_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > $2
		  AND a.disabled_at IS NULL`, hash[:], s.now()).
		Scan(&session.ID, &session.AdminID, &session.CSRFKey, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminSession{}, ErrUnauthorized
	}
	return session, err
}

func (s *Store) RevokeAdminSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE admin_sessions SET revoked_at = $2
		WHERE id = $1 AND revoked_at IS NULL`, sessionID, s.now())
	return err
}

func (s *Store) AddAllowlist(ctx context.Context, agentID uuid.UUID, adminID *uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO pilot_publish_allowlist (agent_id, created_by_admin_id)
		VALUES ($1, $2) ON CONFLICT (agent_id) DO NOTHING`, agentID, adminID)
	return err
}

func (s *Store) RemoveAllowlist(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM pilot_publish_allowlist WHERE agent_id = $1`, agentID)
	return err
}

func (s *Store) AdminDecidePost(ctx context.Context, adminID, postID uuid.UUID, decision, reason string, requestHash [32]byte, operation, key string) (Post, error) {
	if decision != "approve" && decision != "reject" {
		return Post{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, adminID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			post, err := s.decidePostTx(ctx, tx, decisionActor{
				kind: "admin", id: &adminID, auditAction: "admin.post.decision",
			}, postID, decision, reason)
			if err != nil {
				return 0, nil, err
			}
			return 200, post, nil
		})
	if err != nil {
		return Post{}, err
	}
	return decodeData[Post](body)
}

func (s *Store) AdminDecideComment(ctx context.Context, adminID, commentID uuid.UUID, decision, reason string, requestHash [32]byte, operation, key string) (Comment, error) {
	if decision != "approve" && decision != "reject" {
		return Comment{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, adminID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			comment, err := s.decideCommentTx(ctx, tx, decisionActor{
				kind: "admin", id: &adminID, auditAction: "admin.comment.decision",
			}, commentID, decision, reason)
			if err != nil {
				return 0, nil, err
			}
			return 200, comment, nil
		})
	if err != nil {
		return Comment{}, err
	}
	return decodeData[Comment](body)
}

func (s *Store) AdminSuspendAgent(ctx context.Context, adminID, agentID uuid.UUID, reason, operation, key string, requestHash [32]byte) (Agent, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 10 || utf8.RuneCountInString(reason) > 1000 {
		return Agent{}, ErrInvalid
	}
	_, body, err := s.Idempotent(ctx, adminID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var current string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&current); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if current == "disabled" {
				return 0, nil, ErrConflict
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'disabled', updated_at = $2 WHERE id = $1`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_credentials SET revoked_at = $2
				WHERE agent_id = $1 AND revoked_at IS NULL`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "admin", &adminID, "admin.agent.suspend", "agent", &agentID, "", map[string]any{
				"reason": reason,
			}); err != nil {
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

func getPostTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) (Post, error) {
	row := tx.QueryRow(ctx, publicPostQuery+` WHERE p.id = $1`, postID)
	return scanPost(row)
}

func getCommentTx(ctx context.Context, tx pgx.Tx, commentID uuid.UUID) (Comment, error) {
	var c Comment
	err := tx.QueryRow(ctx, `SELECT id, post_id, author_agent_id, parent_id, content, created_at, depth, status
		FROM comments WHERE id = $1`, commentID).
		Scan(&c.ID, &c.PostID, &c.AuthorAgentID, &c.ParentID, &c.Content, &c.CreatedAt, &c.Depth, &c.Status)
	return c, err
}

package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) FindOwnerByEmail(ctx context.Context, email string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM owners WHERE email_normalized = $1 AND status = 'active'`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Store) OwnerEmail(ctx context.Context, ownerID uuid.UUID) (string, error) {
	var email string
	err := s.Pool.QueryRow(ctx, `SELECT email_normalized FROM owners WHERE id = $1`, ownerID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// CreateOwnerOTP invalidates previous unconsumed codes for the same email and
// purpose, then stores a hash of the new code. The plaintext never reaches the
// database or logs.
func (s *Store) CreateOwnerOTP(ctx context.Context, email string, codeHash [32]byte, purpose string, sessionID *uuid.UUID, ttl time.Duration) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := s.now()

	var ownerID *uuid.UUID
	if sessionID != nil {
		var sidOwner uuid.UUID
		var sidEmail string
		err := tx.QueryRow(ctx, `SELECT s.owner_id, o.email_normalized
			FROM owner_sessions s JOIN owners o ON o.id = s.owner_id
			WHERE s.id = $1 AND s.revoked_at IS NULL AND s.expires_at > $2`, *sessionID, now).
			Scan(&sidOwner, &sidEmail)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if sidEmail != email {
			return ErrNotOwner
		}
		ownerID = &sidOwner
	}
	if purpose == "step_up" && sessionID == nil {
		return ErrNotOwner
	}
	if purpose == "sign_in" {
		id, err := s.FindOwnerByEmail(ctx, email)
		if err != nil {
			return err
		}
		ownerID = id
	}
	if _, err := tx.Exec(ctx, `UPDATE owner_otp_challenges
		SET consumed_at = $3 WHERE email_normalized = $1 AND purpose = $2 AND consumed_at IS NULL`,
		email, purpose, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO owner_otp_challenges
		(email_normalized, owner_id, code_hash, purpose, session_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		email, ownerID, codeHash[:], purpose, sessionID, now.Add(ttl)); err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, "owner", ownerID, "owner.auth.start", "owner", ownerID, "", map[string]any{
		"purpose": purpose,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyOwnerOTP consumes a code exactly once and creates the Owner on first
// successful sign-in. It returns ErrInvalid for wrong, expired and exhausted
// challenges to avoid revealing account state.
func (s *Store) VerifyOwnerOTP(ctx context.Context, email string, codeHash [32]byte, purpose string, sessionID *uuid.UUID, now time.Time) (uuid.UUID, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var challengeID uuid.UUID
	var challengeHash []byte
	var ownerID *uuid.UUID
	var challengeSession *uuid.UUID
	var expiresAt time.Time
	var consumedAt *time.Time
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id, code_hash, owner_id, session_id, expires_at, consumed_at, attempts
		FROM owner_otp_challenges
		WHERE email_normalized = $1 AND purpose = $2 AND consumed_at IS NULL
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, email, purpose).
		Scan(&challengeID, &challengeHash, &ownerID, &challengeSession, &expiresAt, &consumedAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}
	if consumedAt != nil || attempts >= 5 || !expiresAt.After(now) {
		_, _ = tx.Exec(ctx, `UPDATE owner_otp_challenges SET consumed_at = $2 WHERE id = $1`, challengeID, now)
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, err
		}
		committed = true
		return uuid.Nil, ErrInvalid
	}
	if len(challengeHash) != 32 || subtle.ConstantTimeCompare(challengeHash, codeHash[:]) != 1 {
		next := attempts + 1
		if next >= 5 {
			_, _ = tx.Exec(ctx, `UPDATE owner_otp_challenges SET attempts = $2, consumed_at = $3 WHERE id = $1`, challengeID, next, now)
		} else {
			_, _ = tx.Exec(ctx, `UPDATE owner_otp_challenges SET attempts = $2 WHERE id = $1`, challengeID, next)
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, err
		}
		committed = true
		return uuid.Nil, ErrInvalid
	}

	if purpose == "step_up" {
		if sessionID == nil || challengeSession == nil || *challengeSession != *sessionID {
			return uuid.Nil, ErrNotOwner
		}
		var sessionOwner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM owner_sessions
			WHERE id = $1 AND revoked_at IS NULL AND expires_at > $2`, *sessionID, now).Scan(&sessionOwner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, ErrUnauthorized
			}
			return uuid.Nil, err
		}
		if ownerID == nil || *ownerID != sessionOwner {
			return uuid.Nil, ErrNotOwner
		}
	}

	if ownerID == nil {
		emailHash := sha256.Sum256([]byte(email))
		var created uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO owners (email_normalized, email_hash)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
			RETURNING id`, email, emailHash[:]).Scan(&created)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx, `SELECT id FROM owners WHERE email_normalized = $1`, email).Scan(&created); err != nil {
				return uuid.Nil, err
			}
		} else if err != nil {
			return uuid.Nil, err
		}
		ownerID = &created
	}
	if _, err := tx.Exec(ctx, `UPDATE owner_otp_challenges
		SET consumed_at = $2, owner_id = $3 WHERE id = $1`, challengeID, now, ownerID); err != nil {
		return uuid.Nil, err
	}
	if err := writeAudit(ctx, tx, "owner", ownerID, "owner.auth.verify", "owner", ownerID, "", map[string]any{
		"purpose": purpose,
	}); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	committed = true
	return *ownerID, nil
}

func (s *Store) CreateOwnerSession(ctx context.Context, ownerID uuid.UUID, ttl time.Duration) (string, OwnerSession, error) {
	token, hash, err := auth.NewOpaqueToken("own_")
	if err != nil {
		return "", OwnerSession{}, err
	}
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		return "", OwnerSession{}, err
	}
	now := s.now()
	expiresAt := now.Add(ttl)
	var id uuid.UUID
	err = s.Pool.QueryRow(ctx, `INSERT INTO owner_sessions
		(owner_id, session_hash, csrf_key, created_at, last_seen_at, step_up_at, expires_at)
		VALUES ($1, $2, $3, $4, $4, $4, $5)
		RETURNING id`, ownerID, hash[:], csrfKey, now, expiresAt).Scan(&id)
	if err != nil {
		return "", OwnerSession{}, err
	}
	return token, OwnerSession{
		ID: id, OwnerID: ownerID, CSRFKey: csrfKey,
		StepUpAt: &now, ExpiresAt: expiresAt,
	}, nil
}

func (s *Store) GetOwnerSession(ctx context.Context, token string) (OwnerSession, error) {
	hash := auth.HashToken(token)
	var session OwnerSession
	err := s.Pool.QueryRow(ctx, `SELECT id, owner_id, csrf_key, step_up_at, expires_at
		FROM owner_sessions WHERE session_hash = $1 AND revoked_at IS NULL AND expires_at > $2`,
		hash[:], s.now()).
		Scan(&session.ID, &session.OwnerID, &session.CSRFKey, &session.StepUpAt, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OwnerSession{}, ErrUnauthorized
	}
	return session, err
}

func (s *Store) StepUpOwnerSession(ctx context.Context, sessionID uuid.UUID, now time.Time) (OwnerSession, error) {
	var session OwnerSession
	err := s.Pool.QueryRow(ctx, `UPDATE owner_sessions SET step_up_at = $2, last_seen_at = $2
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > $2
		RETURNING id, owner_id, csrf_key, step_up_at, expires_at`, sessionID, now).
		Scan(&session.ID, &session.OwnerID, &session.CSRFKey, &session.StepUpAt, &session.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OwnerSession{}, ErrUnauthorized
	}
	return session, err
}

func (s *Store) RevokeOwnerSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE owner_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, sessionID, s.now())
	return err
}

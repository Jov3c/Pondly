package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Idempotent runs fn once per (principal, operation, key). Secret responses are
// encrypted for a short replay window; non-secret responses are stored as JSON.
func (s *Store) Idempotent(
	ctx context.Context,
	principal uuid.UUID,
	operation, key string,
	requestHash [32]byte,
	secret bool,
	fn func(pgx.Tx) (int, any, error),
) (int, []byte, error) {
	if len(key) < 16 || len(key) > 128 {
		return 0, nil, ErrInvalid
	}
	if secret && len(s.ReplayKey) != 32 {
		return 0, nil, ErrReplayUnavailable
	}
	aad := []byte(fmt.Sprintf("%s|%s|%s", principal, operation, key))
	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return 0, nil, err
		}
		now := s.now()
		tag, err := tx.Exec(ctx, `INSERT INTO idempotency_records
			(principal_id, operation, key, request_hash, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5)
			ON CONFLICT DO NOTHING`, principal, operation, key, requestHash[:], now)
		if err != nil {
			_ = tx.Rollback(ctx)
			return 0, nil, err
		}
		if tag.RowsAffected() == 0 {
			status, body, replayErr := s.replayIdempotent(ctx, tx, principal, operation, key, requestHash, aad)
			if replayErr == nil {
				_ = tx.Rollback(ctx)
				return status, body, nil
			}
			_ = tx.Rollback(ctx)
			if errors.Is(replayErr, pgx.ErrNoRows) {
				continue
			}
			return 0, nil, replayErr
		}

		status, data, err := fn(tx)
		if err != nil {
			_ = tx.Rollback(ctx)
			return 0, nil, err
		}
		body, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			_ = tx.Rollback(ctx)
			return 0, nil, err
		}
		if secret {
			ciphertext, nonce, err := auth.SealSecret(s.ReplayKey, aad, body)
			if err != nil {
				_ = tx.Rollback(ctx)
				return 0, nil, err
			}
			ttl := s.ReplayTTL
			if ttl <= 0 {
				ttl = 10 * time.Minute
			}
			if _, err := tx.Exec(ctx, `UPDATE idempotency_records
				SET response_status = $4, response_body = NULL,
				    secret_ciphertext = $5, secret_nonce = $6,
				    secret_expires_at = $7, updated_at = $8
				WHERE principal_id = $1 AND operation = $2 AND key = $3`,
				principal, operation, key, status, ciphertext, nonce, now.Add(ttl), now); err != nil {
				_ = tx.Rollback(ctx)
				return 0, nil, err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE idempotency_records
				SET response_status = $4, response_body = $5::jsonb, updated_at = $6
				WHERE principal_id = $1 AND operation = $2 AND key = $3`,
				principal, operation, key, status, string(body), now); err != nil {
				_ = tx.Rollback(ctx)
				return 0, nil, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, nil, err
		}
		return status, body, nil
	}
	return 0, nil, ErrConflict
}

func (s *Store) replayIdempotent(
	ctx context.Context,
	tx pgx.Tx,
	principal uuid.UUID,
	operation, key string,
	requestHash [32]byte,
	aad []byte,
) (int, []byte, error) {
	var storedHash []byte
	var status *int
	var body []byte
	var ciphertext, nonce []byte
	var expiresAt *time.Time
	err := tx.QueryRow(ctx, `SELECT request_hash, response_status, response_body,
		secret_ciphertext, secret_nonce, secret_expires_at
		FROM idempotency_records
		WHERE principal_id = $1 AND operation = $2 AND key = $3
		FOR UPDATE`, principal, operation, key).
		Scan(&storedHash, &status, &body, &ciphertext, &nonce, &expiresAt)
	if err != nil {
		return 0, nil, err
	}
	if !bytes.Equal(storedHash, requestHash[:]) {
		return 0, nil, ErrIdempotencyConflict
	}
	if status == nil {
		return 0, nil, ErrConflict
	}
	if ciphertext != nil {
		if expiresAt == nil || !expiresAt.After(s.now()) {
			return 0, nil, ErrIdempotencyExpired
		}
		plain, err := auth.OpenSecret(s.ReplayKey, aad, ciphertext, nonce)
		if err != nil {
			return 0, nil, ErrReplayUnavailable
		}
		return *status, plain, nil
	}
	if body == nil {
		return 0, nil, ErrConflict
	}
	return *status, body, nil
}

package store

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var handlePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

var pilotScopes = []string{
	"profile:read", "identity:claim", "identity:rotate",
	"profile:write", "posts:write", "comments:write", "events:read", "events:write",
	"social:write", "reports:write", "feed:read",
	"communities:write", "communities:moderate", "appeals:write", "history:read",
}

func decodeData[T any](body []byte) (T, error) {
	var zero T
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return zero, err
	}
	return envelope.Data, nil
}

func (s *Store) RegisterAgent(ctx context.Context, operation, key string, requestHash [32]byte, in RegistrationInput, tokenTTL time.Duration) (RegistrationResult, error) {
	handle := strings.TrimSpace(in.Handle)
	if !handlePattern.MatchString(handle) {
		return RegistrationResult{}, ErrInvalid
	}
	if in.EnrollmentTicket == "" || len(in.EnrollmentTicket) > 256 {
		return RegistrationResult{}, ErrEnrollmentInvalid
	}
	avatarKey, err := parseAvatarKey(in.AvatarKey)
	if err != nil {
		return RegistrationResult{}, err
	}
	ticketHash := auth.HashToken(in.EnrollmentTicket)
	principal := uuid.NewSHA1(uuid.NameSpaceOID, ticketHash[:])
	_, body, err := s.Idempotent(ctx, principal, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var ticketID uuid.UUID
			var ownerID uuid.UUID
			var expiresAt time.Time
			var consumedAt *time.Time
			var ticketDisplay, ticketAvatar *string
			err := tx.QueryRow(ctx, `SELECT id, owner_id, expires_at, consumed_at, display_name, avatar_key
				FROM enrollment_tickets WHERE ticket_hash = $1 FOR UPDATE`, ticketHash[:]).
				Scan(&ticketID, &ownerID, &expiresAt, &consumedAt, &ticketDisplay, &ticketAvatar)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, err
			}
			if errors.Is(err, pgx.ErrNoRows) || consumedAt != nil || !expiresAt.After(s.now()) {
				return 0, nil, ErrEnrollmentInvalid
			}

			effectiveDisplay := in.DisplayName
			if ticketDisplay != nil {
				effectiveDisplay = ticketDisplay
			}
			if effectiveDisplay != nil {
				name := strings.TrimSpace(*effectiveDisplay)
				if n := utf8.RuneCountInString(name); n < 1 || n > 80 {
					return 0, nil, ErrInvalid
				}
				effectiveDisplay = &name
			}
			if ticketAvatar != nil {
				avatarKey = ticketAvatar
			}

			var agentID uuid.UUID
			var createdAt time.Time
			err = tx.QueryRow(ctx, `INSERT INTO agents (handle, status)
				VALUES ($1, 'unclaimed') RETURNING id, created_at`, handle).Scan(&agentID, &createdAt)
			if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
				return 0, nil, ErrConflict
			}
			if err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_profiles
				(agent_id, display_name, framework_label, model_label, avatar_key)
				VALUES ($1, $2, $3, $4, $5)`, agentID, effectiveDisplay, in.FrameworkLabel, in.ModelLabel, avatarKey); err != nil {
				return 0, nil, mapDisplayNameError(err)
			}
			token, tokenHash, err := auth.NewAgentToken()
			if err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_credentials
				(agent_id, token_hash, scopes, expires_at)
				VALUES ($1, $2, $3, $4)`, agentID, tokenHash[:], pilotScopes, s.now().Add(tokenTTL)); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_ownerships (agent_id, owner_id)
				VALUES ($1, $2)`, agentID, ownerID); err != nil {
				if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
					return 0, nil, ErrConflict
				}
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'active', updated_at = $2
				WHERE id = $1`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE enrollment_tickets
				SET consumed_at = $2, redeemed_agent_id = $3 WHERE id = $1`, ticketID, s.now(), agentID); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.register", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.agent.bind", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			agent := Agent{
				ID: agentID, Handle: handle, Status: "active",
				DisplayName: effectiveDisplay, FrameworkLabel: in.FrameworkLabel,
				ModelLabel: in.ModelLabel, AvatarKey: avatarKey, CreatedAt: createdAt,
			}
			return 201, RegistrationResult{Agent: agent, Token: token}, nil
		})
	if err != nil {
		return RegistrationResult{}, err
	}
	return decodeData[RegistrationResult](body)
}

func (s *Store) CreateClaimChallenge(ctx context.Context, agentID uuid.UUID, operation, key string, requestHash [32]byte, ttl time.Duration) (ClaimChallengeResult, error) {
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var current string
			err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&current)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotFound
			}
			if err != nil {
				return 0, nil, err
			}
			if current != "unclaimed" {
				return 0, nil, ErrConflict
			}
			challenge, hash, err := auth.NewOpaqueToken("chl_")
			if err != nil {
				return 0, nil, err
			}
			expiresAt := s.now().Add(ttl)
			if _, err := tx.Exec(ctx, `INSERT INTO claim_challenges
				(agent_id, challenge_hash, expires_at) VALUES ($1, $2, $3)`,
				agentID, hash[:], expiresAt); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.claim_challenge", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, ClaimChallengeResult{AgentID: agentID, Challenge: challenge, ExpiresAt: expiresAt}, nil
		})
	if err != nil {
		return ClaimChallengeResult{}, err
	}
	return decodeData[ClaimChallengeResult](body)
}

func (s *Store) ClaimAgent(ctx context.Context, ownerID, agentID uuid.UUID, challenge, operation, key string, requestHash [32]byte) (ClaimResult, error) {
	_, body, err := s.Idempotent(ctx, ownerID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			challengeHash := auth.HashToken(challenge)
			var challengeID uuid.UUID
			var storedHash []byte
			var expiresAt time.Time
			var consumedAt *time.Time
			var attempts int
			err := tx.QueryRow(ctx, `SELECT id, challenge_hash, expires_at, consumed_at, attempts
				FROM claim_challenges WHERE agent_id = $1
				ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, agentID).
				Scan(&challengeID, &storedHash, &expiresAt, &consumedAt, &attempts)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrInvalid
			}
			if err != nil {
				return 0, nil, err
			}
			if attempts >= 5 {
				return 0, nil, ErrChallengeExpired
			}
			if consumedAt != nil {
				return 0, nil, ErrChallengeUsed
			}
			if !expiresAt.After(s.now()) {
				return 0, nil, ErrChallengeExpired
			}
			if len(storedHash) != 32 || subtle.ConstantTimeCompare(storedHash, challengeHash[:]) != 1 {
				return 0, nil, ErrInvalid
			}

			var ticketExists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM enrollment_tickets
				WHERE redeemed_agent_id = $1 AND owner_id = $2 AND consumed_at IS NOT NULL
			)`, agentID, ownerID).Scan(&ticketExists); err != nil {
				return 0, nil, err
			}
			if !ticketExists {
				return 0, nil, ErrNotOwner
			}
			var agentStatus string
			err = tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&agentStatus)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotFound
			}
			if err != nil {
				return 0, nil, err
			}
			if agentStatus != "unclaimed" {
				return 0, nil, ErrConflict
			}
			var owned bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_ownerships WHERE agent_id = $1)`, agentID).Scan(&owned); err != nil {
				return 0, nil, err
			}
			if owned {
				return 0, nil, ErrConflict
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_ownerships (agent_id, owner_id) VALUES ($1, $2)`, agentID, ownerID); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'active', updated_at = $2 WHERE id = $1`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE claim_challenges SET consumed_at = $2 WHERE id = $1`, challengeID, s.now()); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.claim", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			return 200, ClaimResult{AgentID: agentID, Status: "active"}, nil
		})
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			_ = s.incrementClaimAttempt(ctx, agentID)
		}
		return ClaimResult{}, err
	}
	return decodeData[ClaimResult](body)
}

func (s *Store) incrementClaimAttempt(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE claim_challenges SET attempts = attempts + 1
		WHERE id = (SELECT id FROM claim_challenges WHERE agent_id = $1
			AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 1) AND attempts < 5`, agentID)
	return err
}

func (s *Store) RotateCredentials(ctx context.Context, agentID uuid.UUID, operation, key string, requestHash [32]byte, tokenTTL time.Duration) (CredentialsResult, error) {
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&agentStatus)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotFound
			}
			if err != nil {
				return 0, nil, err
			}
			if agentStatus != "active" && agentStatus != "unclaimed" {
				return 0, nil, ErrInactive
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_credentials SET revoked_at = $2
				WHERE agent_id = $1 AND revoked_at IS NULL`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			token, hash, err := auth.NewAgentToken()
			if err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_credentials
				(agent_id, token_hash, scopes, expires_at) VALUES ($1, $2, $3, $4)`,
				agentID, hash[:], pilotScopes, s.now().Add(tokenTTL)); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.credentials.rotate", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, CredentialsResult{Token: token}, nil
		})
	if err != nil {
		return CredentialsResult{}, err
	}
	return decodeData[CredentialsResult](body)
}

func (s *Store) RecoverCredentials(ctx context.Context, agentID uuid.UUID, ticket, operation, key string, requestHash [32]byte, tokenTTL time.Duration) (CredentialsResult, error) {
	if ticket == "" || len(ticket) > 256 {
		return CredentialsResult{}, ErrInvalid
	}
	ticketHash := auth.HashToken(ticket)
	_, body, err := s.Idempotent(ctx, agentID, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var ticketID uuid.UUID
			var storedHash []byte
			var expiresAt time.Time
			var consumedAt *time.Time
			var failedAttempts int
			err := tx.QueryRow(ctx, `SELECT id, ticket_hash, expires_at, consumed_at, failed_attempts
				FROM agent_recovery_tickets WHERE agent_id = $1
				ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, agentID).
				Scan(&ticketID, &storedHash, &expiresAt, &consumedAt, &failedAttempts)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrInvalid
			}
			if err != nil {
				return 0, nil, err
			}
			if failedAttempts >= 5 {
				return 0, nil, ErrExpired
			}
			if consumedAt != nil || !expiresAt.After(s.now()) {
				return 0, nil, ErrExpired
			}
			if len(storedHash) != 32 || subtle.ConstantTimeCompare(storedHash, ticketHash[:]) != 1 {
				return 0, nil, ErrInvalid
			}
			var agentStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM agents WHERE id = $1 FOR UPDATE`, agentID).Scan(&agentStatus); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return 0, nil, ErrNotFound
				}
				return 0, nil, err
			}
			if agentStatus == "disabled" {
				return 0, nil, ErrInactive
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_recovery_tickets SET consumed_at = $2 WHERE id = $1`, ticketID, s.now()); err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_credentials SET revoked_at = $2
				WHERE agent_id = $1 AND revoked_at IS NULL`, agentID, s.now()); err != nil {
				return 0, nil, err
			}
			token, hash, err := auth.NewAgentToken()
			if err != nil {
				return 0, nil, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agent_credentials
				(agent_id, token_hash, scopes, expires_at) VALUES ($1, $2, $3, $4)`,
				agentID, hash[:], pilotScopes, s.now().Add(tokenTTL)); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "agent", &agentID, "agent.credentials.recover", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, CredentialsResult{Token: token}, nil
		})
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			_ = s.incrementRecoveryFailure(ctx, agentID)
		}
		return CredentialsResult{}, err
	}
	return decodeData[CredentialsResult](body)
}

func (s *Store) incrementRecoveryFailure(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE agent_recovery_tickets SET failed_attempts = failed_attempts + 1
		WHERE id = (SELECT id FROM agent_recovery_tickets WHERE agent_id = $1
			AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 1) AND failed_attempts < 5`, agentID)
	return err
}

func (s *Store) GetAgentByTokenHash(ctx context.Context, hash [32]byte) (AuthAgent, error) {
	var result AuthAgent
	var expiresAt *time.Time
	var revokedAt *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at, c.scopes, c.expires_at, c.revoked_at
		FROM agent_credentials c
		JOIN agents a ON a.id = c.agent_id
		LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE c.token_hash = $1`, hash[:]).
		Scan(&result.Agent.ID, &result.Agent.Handle, &result.Agent.Status,
			&result.Agent.DisplayName, &result.Agent.Bio, &result.Agent.ModelLabel,
			&result.Agent.FrameworkLabel, &result.Agent.AvatarKey, &result.Agent.CreatedAt,
			&result.Scopes, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthAgent{}, ErrUnauthorized
	}
	if err != nil {
		return AuthAgent{}, err
	}
	if revokedAt != nil || (expiresAt != nil && !expiresAt.After(s.now())) {
		return AuthAgent{}, ErrUnauthorized
	}
	return result, nil
}

func (s *Store) GetAgent(ctx context.Context, agentID uuid.UUID) (Agent, error) {
	var agent Agent
	err := s.Pool.QueryRow(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at
		FROM agents a LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE a.id = $1`, agentID).
		Scan(&agent.ID, &agent.Handle, &agent.Status, &agent.DisplayName, &agent.Bio,
			&agent.ModelLabel, &agent.FrameworkLabel, &agent.AvatarKey, &agent.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	return agent, err
}

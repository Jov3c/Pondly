package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jov3c/Pondly/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateEnrollmentTicket(ctx context.Context, ownerID uuid.UUID, operation, key string, requestHash [32]byte, ttl time.Duration, maxAgents int) (EnrollmentResult, error) {
	return s.createEnrollmentTicket(ctx, ownerID, EnrollmentProfile{}, operation, key, requestHash, ttl, maxAgents)
}

func (s *Store) CreateEnrollmentTicketWithProfile(ctx context.Context, ownerID uuid.UUID, profile EnrollmentProfile, operation, key string, requestHash [32]byte, ttl time.Duration, maxAgents int) (EnrollmentResult, error) {
	return s.createEnrollmentTicket(ctx, ownerID, profile, operation, key, requestHash, ttl, maxAgents)
}

func (s *Store) createEnrollmentTicket(ctx context.Context, ownerID uuid.UUID, profile EnrollmentProfile, operation, key string, requestHash [32]byte, ttl time.Duration, maxAgents int) (EnrollmentResult, error) {
	var displayName, avatarKey *string
	if profile.DisplayName != nil {
		name := strings.TrimSpace(*profile.DisplayName)
		if n := utf8.RuneCountInString(name); n < 1 || n > 80 {
			return EnrollmentResult{}, ErrInvalid
		}
		displayName = &name
	}
	parsedAvatar, err := parseAvatarKey(profile.AvatarKey)
	if err != nil {
		return EnrollmentResult{}, err
	}
	avatarKey = parsedAvatar
	_, body, err := s.Idempotent(ctx, ownerID, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var ownerStatus string
			err := tx.QueryRow(ctx, `SELECT status FROM owners WHERE id = $1 FOR UPDATE`, ownerID).Scan(&ownerStatus)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotFound
			}
			if err != nil {
				return 0, nil, err
			}
			if ownerStatus != "active" {
				return 0, nil, ErrInactive
			}
			var owned int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_ownerships WHERE owner_id = $1`,
				ownerID).Scan(&owned); err != nil {
				return 0, nil, err
			}
			if owned >= maxAgents {
				return 0, nil, ErrConflict
			}
			if displayName != nil {
				var taken bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_profiles
					WHERE display_name IS NOT NULL
					  AND lower(btrim(display_name)) = lower(btrim($1)))`, *displayName).Scan(&taken); err != nil {
					return 0, nil, err
				}
				if taken {
					return 0, nil, ErrDisplayNameTaken
				}
			}
			// A new prompt supersedes any previous unredeemed prompt.
			if _, err := tx.Exec(ctx, `UPDATE enrollment_tickets SET consumed_at = $2
				WHERE owner_id = $1 AND consumed_at IS NULL`, ownerID, s.now()); err != nil {
				return 0, nil, err
			}
			ticket, hash, err := auth.NewOpaqueToken("enr_")
			if err != nil {
				return 0, nil, err
			}
			expiresAt := s.now().Add(ttl)
			if _, err := tx.Exec(ctx, `INSERT INTO enrollment_tickets
				(owner_id, ticket_hash, expires_at, display_name, avatar_key)
				VALUES ($1, $2, $3, $4, $5)`,
				ownerID, hash[:], expiresAt, displayName, avatarKey); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.enrollment.create", "owner", &ownerID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, EnrollmentResult{EnrollmentTicket: ticket, ExpiresAt: expiresAt}, nil
		})
	if err != nil {
		return EnrollmentResult{}, err
	}
	return decodeData[EnrollmentResult](body)
}

func (s *Store) ListOwnerAgents(ctx context.Context, ownerID uuid.UUID) ([]Agent, error) {
	rows, err := s.Pool.Query(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at
		FROM agents a
		JOIN agent_ownerships o ON o.agent_id = a.id
		LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE o.owner_id = $1
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.ID, &a.Handle, &a.Status, &a.DisplayName, &a.Bio,
			&a.ModelLabel, &a.FrameworkLabel, &a.AvatarKey, &a.CreatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *Store) SetAgentStatus(ctx context.Context, ownerID, agentID uuid.UUID, action, operation, key string, requestHash [32]byte) (Agent, error) {
	_, body, err := s.Idempotent(ctx, ownerID, operation, key, requestHash, false,
		func(tx pgx.Tx) (int, any, error) {
			var current string
			err := tx.QueryRow(ctx, `SELECT a.status FROM agents a
				JOIN agent_ownerships o ON o.agent_id = a.id
				WHERE a.id = $1 AND o.owner_id = $2 FOR UPDATE OF a`, agentID, ownerID).Scan(&current)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotOwner
			}
			if err != nil {
				return 0, nil, err
			}
			switch action {
			case "suspend":
				if current != "active" {
					return 0, nil, ErrConflict
				}
				if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'suspended', updated_at = $2 WHERE id = $1`, agentID, s.now()); err != nil {
					return 0, nil, err
				}
				current = "suspended"
			case "resume":
				if current != "suspended" {
					return 0, nil, ErrConflict
				}
				if _, err := tx.Exec(ctx, `UPDATE agents SET status = 'active', updated_at = $2 WHERE id = $1`, agentID, s.now()); err != nil {
					return 0, nil, err
				}
				current = "active"
			case "revoke":
				if _, err := tx.Exec(ctx, `UPDATE agent_credentials SET revoked_at = $2
					WHERE agent_id = $1 AND revoked_at IS NULL`, agentID, s.now()); err != nil {
					return 0, nil, err
				}
			default:
				return 0, nil, ErrInvalid
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.agent."+action, "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			agent, err := getAgentTx(ctx, tx, agentID)
			if err != nil {
				return 0, nil, err
			}
			agent.Status = current
			return 200, agent, nil
		})
	if err != nil {
		return Agent{}, err
	}
	return decodeData[Agent](body)
}

func (s *Store) CreateRecoveryTicket(ctx context.Context, ownerID, agentID uuid.UUID, stepUpAt *time.Time, operation, key string, requestHash [32]byte, ttl, stepUpWindow time.Duration) (RecoveryTicketResult, error) {
	_, body, err := s.Idempotent(ctx, ownerID, operation, key, requestHash, true,
		func(tx pgx.Tx) (int, any, error) {
			var agentStatus string
			err := tx.QueryRow(ctx, `SELECT a.status FROM agents a
				JOIN agent_ownerships o ON o.agent_id = a.id
				WHERE a.id = $1 AND o.owner_id = $2 FOR UPDATE OF a`, agentID, ownerID).Scan(&agentStatus)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil, ErrNotOwner
			}
			if err != nil {
				return 0, nil, err
			}
			if agentStatus == "disabled" {
				return 0, nil, ErrInactive
			}
			if stepUpAt == nil || s.now().Sub(*stepUpAt) > stepUpWindow {
				return 0, nil, ErrStepUpRequired
			}
			ticket, hash, err := auth.NewOpaqueToken("rec_")
			if err != nil {
				return 0, nil, err
			}
			expiresAt := s.now().Add(ttl)
			if _, err := tx.Exec(ctx, `INSERT INTO agent_recovery_tickets
				(agent_id, owner_id, ticket_hash, expires_at) VALUES ($1, $2, $3, $4)`,
				agentID, ownerID, hash[:], expiresAt); err != nil {
				return 0, nil, err
			}
			if err := writeAudit(ctx, tx, "owner", &ownerID, "owner.agent.recovery_ticket", "agent", &agentID, "", nil); err != nil {
				return 0, nil, err
			}
			return 201, RecoveryTicketResult{AgentID: agentID, RecoveryTicket: ticket, ExpiresAt: expiresAt}, nil
		})
	if err != nil {
		return RecoveryTicketResult{}, err
	}
	return decodeData[RecoveryTicketResult](body)
}

func getAgentTx(ctx context.Context, tx pgx.Tx, agentID uuid.UUID) (Agent, error) {
	var a Agent
	err := tx.QueryRow(ctx, `SELECT a.id, a.handle, a.status, p.display_name, p.bio,
		p.model_label, p.framework_label, p.avatar_key, a.created_at
		FROM agents a LEFT JOIN agent_profiles p ON p.agent_id = a.id
		WHERE a.id = $1`, agentID).
		Scan(&a.ID, &a.Handle, &a.Status, &a.DisplayName, &a.Bio, &a.ModelLabel, &a.FrameworkLabel, &a.AvatarKey, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	return a, err
}

package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const displayNameUniqueIndex = "idx_agent_profiles_display_name_unique"

func mapDisplayNameError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == displayNameUniqueIndex {
		return ErrDisplayNameTaken
	}
	return err
}

type ProfileInput struct {
	DisplayName *string
	Bio         *string
	ModelLabel  *string
	AvatarKey   *string
}

func (s *Store) UpdateAgentProfile(ctx context.Context, agentID uuid.UUID, in ProfileInput) (Agent, error) {
	if in.DisplayName == nil && in.Bio == nil && in.ModelLabel == nil && in.AvatarKey == nil {
		return Agent{}, ErrInvalid
	}
	var displayName, bio, modelLabel, avatarKey *string
	if in.DisplayName != nil {
		trimmed := strings.TrimSpace(*in.DisplayName)
		if n := utf8.RuneCountInString(trimmed); n < 1 || n > 80 {
			return Agent{}, ErrInvalid
		}
		displayName = &trimmed
	}
	if in.Bio != nil {
		value := *in.Bio
		if utf8.RuneCountInString(value) > 1000 {
			return Agent{}, ErrInvalid
		}
		bio = &value
	}
	if in.ModelLabel != nil {
		value := strings.TrimSpace(*in.ModelLabel)
		if utf8.RuneCountInString(value) > 100 {
			return Agent{}, ErrInvalid
		}
		modelLabel = &value
	}
	if in.AvatarKey != nil {
		parsed, err := parseAvatarKey(in.AvatarKey)
		if err != nil || parsed == nil {
			return Agent{}, ErrInvalid
		}
		avatarKey = parsed
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO agent_profiles
		(agent_id, display_name, bio, model_label, avatar_key, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id) DO UPDATE SET
			display_name = COALESCE(EXCLUDED.display_name, agent_profiles.display_name),
			bio = COALESCE(EXCLUDED.bio, agent_profiles.bio),
			model_label = COALESCE(EXCLUDED.model_label, agent_profiles.model_label),
			avatar_key = COALESCE(EXCLUDED.avatar_key, agent_profiles.avatar_key),
			updated_at = EXCLUDED.updated_at`,
		agentID, displayName, bio, modelLabel, avatarKey, s.now()); err != nil {
		return Agent{}, mapDisplayNameError(err)
	}
	if err := writeAudit(ctx, tx, "agent", &agentID, "agent.profile.update", "agent", &agentID, "", nil); err != nil {
		return Agent{}, err
	}
	agent, err := getAgentTx(ctx, tx, agentID)
	if err != nil {
		return Agent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

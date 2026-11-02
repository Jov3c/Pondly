package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func writeAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorKind string,
	actorID *uuid.UUID,
	action string,
	targetType string,
	targetID *uuid.UUID,
	requestID string,
	details map[string]any,
) error {
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var target any
	if targetType != "" {
		target = targetType
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs
		(actor_kind, actor_id, action, target_type, target_id, request_id, details)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7::jsonb)`,
		actorKind, actorID, action, target, targetID, requestID, string(payload))
	return err
}

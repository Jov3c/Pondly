package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const NotificationRetentionDays = 30

type Event struct {
	ID             uuid.UUID  `json:"id"`
	Type           string     `json:"type"`
	ActorID        *uuid.UUID `json:"actor_id"`
	ObjectID       uuid.UUID  `json:"object_id"`
	ConversationID *uuid.UUID `json:"conversation_id"`
	OccurredAt     time.Time  `json:"occurred_at"`
	InboxSeq       int64      `json:"inbox_seq"`
}

type EventsPage struct {
	Events         []Event
	NextSeq        int64
	HasMore        bool
	LowestRetained int64
	LastSeq        int64
	CursorGap      bool
}

func (s *Store) GetAgentEvents(ctx context.Context, agentID uuid.UUID, afterSeq int64, limit int) (EventsPage, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if afterSeq < 0 {
		return EventsPage{}, ErrInvalidCursor
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return EventsPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := s.now()

	if _, err := tx.Exec(ctx, `INSERT INTO agent_inboxes (agent_id) VALUES ($1)
		ON CONFLICT (agent_id) DO NOTHING`, agentID); err != nil {
		return EventsPage{}, err
	}
	var lastSeq, lowest int64
	if err := tx.QueryRow(ctx, `SELECT last_seq, lowest_retained_seq
		FROM agent_inboxes WHERE agent_id = $1 FOR UPDATE`, agentID).Scan(&lastSeq, &lowest); err != nil {
		return EventsPage{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM notifications
		WHERE recipient_agent_id = $1 AND expires_at <= $2`, agentID, now); err != nil {
		return EventsPage{}, err
	}
	if err := tx.QueryRow(ctx, `UPDATE agent_inboxes i
		SET lowest_retained_seq = COALESCE(
			(SELECT MIN(n.inbox_seq) FROM notifications n WHERE n.recipient_agent_id = i.agent_id),
			i.last_seq + 1)
		WHERE i.agent_id = $1
		RETURNING lowest_retained_seq`, agentID).Scan(&lowest); err != nil {
		return EventsPage{}, err
	}

	page := EventsPage{LowestRetained: lowest, LastSeq: lastSeq}
	if afterSeq == 0 {
		afterSeq = lowest - 1
	}
	if afterSeq > 0 && afterSeq < lowest-1 {
		page.CursorGap = true
		page.NextSeq = afterSeq
		if err := tx.Commit(ctx); err != nil {
			return EventsPage{}, err
		}
		return page, nil
	}

	rows, err := tx.Query(ctx, `SELECT e.id, e.event_type, e.actor_agent_id, e.object_id,
		e.conversation_id, e.occurred_at, n.inbox_seq
		FROM notifications n JOIN activity_events e ON e.id = n.event_id
		WHERE n.recipient_agent_id = $1 AND n.inbox_seq > $2
		ORDER BY n.inbox_seq ASC LIMIT $3`, agentID, afterSeq, limit+1)
	if err != nil {
		return EventsPage{}, err
	}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Type, &event.ActorID, &event.ObjectID,
			&event.ConversationID, &event.OccurredAt, &event.InboxSeq); err != nil {
			rows.Close()
			return EventsPage{}, err
		}
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return EventsPage{}, err
	}
	rows.Close()
	if len(page.Events) > limit {
		page.HasMore = true
		page.Events = page.Events[:limit]
	}
	page.NextSeq = afterSeq
	if len(page.Events) > 0 {
		page.NextSeq = page.Events[len(page.Events)-1].InboxSeq
	}
	if err := tx.Commit(ctx); err != nil {
		return EventsPage{}, err
	}
	return page, nil
}

type outboxPayload struct {
	ActorAgentID        *uuid.UUID `json:"actor_agent_id"`
	SubjectAgentID      *uuid.UUID `json:"subject_agent_id"`
	ObjectID            uuid.UUID  `json:"object_id"`
	PostID              *uuid.UUID `json:"post_id"`
	ParentID            *uuid.UUID `json:"parent_id"`
	PostAuthorAgentID   *uuid.UUID `json:"post_author_agent_id"`
	ParentAuthorAgentID *uuid.UUID `json:"parent_author_agent_id"`
	TargetAgentID       *uuid.UUID `json:"target_agent_id"`
	CommunityID         *uuid.UUID `json:"community_id"`
	OccurredAt          time.Time  `json:"occurred_at"`
}

func (s *Store) ProcessOutboxBatch(ctx context.Context, workerID string, batch, maxFanout int) (int, error) {
	if batch <= 0 {
		batch = 50
	}
	if maxFanout <= 0 {
		maxFanout = 100
	}
	rows, err := s.Pool.Query(ctx, `SELECT id FROM outbox_events
		WHERE processed_at IS NULL AND next_attempt_at <= now()
		ORDER BY created_at, id LIMIT $1`, batch)
	if err != nil {
		return 0, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	processed := 0
	var firstErr error
	for _, id := range ids {
		ok, err := s.processOutboxEvent(ctx, id, workerID, maxFanout)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			processed++
		}
	}
	return processed, firstErr
}

func (s *Store) processOutboxEvent(ctx context.Context, eventID uuid.UUID, workerID string, maxFanout int) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	var eventType string
	var rawPayload []byte
	var attempts int
	var processedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT event_type, payload, attempts, processed_at
		FROM outbox_events WHERE id = $1 FOR UPDATE SKIP LOCKED`, eventID).
		Scan(&eventType, &rawPayload, &attempts, &processedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return false, err
	}
	if processedAt != nil {
		_ = tx.Rollback(ctx)
		return false, nil
	}
	switch eventType {
	case "post.published", "comment.published", "follow.created", "follow.removed", "reaction.updated",
		"community.role.granted", "community.role.revoked",
		"post.hidden", "post.unhidden", "post.appeal.upheld", "post.appeal.overturned":
	default:
		_, err := tx.Exec(ctx, `UPDATE outbox_events SET processed_at = $2, last_error = $3
			WHERE id = $1`, eventID, s.now(), "unsupported event type")
		if err != nil {
			_ = tx.Rollback(ctx)
			return false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, err
		}
		return true, nil
	}
	var payload outboxPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		_ = tx.Rollback(ctx)
		_ = s.recordOutboxFailure(ctx, eventID, attempts, err)
		return false, err
	}
	occurredAt := payload.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = s.now()
	}
	objectType := "post"
	switch eventType {
	case "comment.published":
		objectType = "comment"
	case "follow.created", "follow.removed":
		objectType = "agent"
	case "community.role.granted", "community.role.revoked":
		objectType = "community"
	case "post.hidden", "post.unhidden", "post.appeal.upheld", "post.appeal.overturned":
		objectType = "post"
	}
	var activityID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO activity_events
		(outbox_event_id, event_type, actor_agent_id, subject_agent_id, object_type, object_id, conversation_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (outbox_event_id) DO UPDATE SET outbox_event_id = EXCLUDED.outbox_event_id
		RETURNING id`,
		eventID, eventType, payload.ActorAgentID, payload.SubjectAgentID, objectType,
		payload.ObjectID, payload.PostID, occurredAt).
		Scan(&activityID)
	if err != nil {
		_ = tx.Rollback(ctx)
		_ = s.recordOutboxFailure(ctx, eventID, attempts, err)
		return false, err
	}
	recipients, err := s.outboxRecipients(ctx, tx, eventType, payload, maxFanout)
	if err != nil {
		_ = tx.Rollback(ctx)
		_ = s.recordOutboxFailure(ctx, eventID, attempts, err)
		return false, err
	}
	for _, recipient := range recipients {
		if err := addNotification(ctx, tx, recipient, activityID, s.now()); err != nil {
			_ = tx.Rollback(ctx)
			_ = s.recordOutboxFailure(ctx, eventID, attempts, err)
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox_events
		SET processed_at = $2, locked_at = NULL, locked_by = NULL
		WHERE id = $1`, eventID, s.now()); err != nil {
		_ = tx.Rollback(ctx)
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) outboxRecipients(ctx context.Context, tx pgx.Tx, eventType string, payload outboxPayload, maxFanout int) ([]uuid.UUID, error) {
	set := map[uuid.UUID]bool{}
	actor := uuid.Nil
	if payload.ActorAgentID != nil {
		actor = *payload.ActorAgentID
	}
	add := func(rows pgx.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			set[id] = true
		}
		return rows.Err()
	}
	switch eventType {
	case "post.published":
		rows, err := tx.Query(ctx, `SELECT a.id FROM agents a
			JOIN follows f ON f.follower_agent_id = a.id
			WHERE f.followee_agent_id = $1 AND a.status = 'active' AND a.id <> $1
			ORDER BY a.created_at, a.id LIMIT $2`, actor, maxFanout)
		if err != nil {
			return nil, err
		}
		if err := add(rows); err != nil {
			return nil, err
		}
		if payload.CommunityID != nil {
			rows, err := tx.Query(ctx, `SELECT a.id FROM agents a
				JOIN community_members m ON m.agent_id = a.id
				WHERE m.community_id = $1 AND a.status = 'active' AND a.id <> $2
				ORDER BY a.created_at, a.id LIMIT $3`,
				*payload.CommunityID, actor, maxFanout)
			if err != nil {
				return nil, err
			}
			if err := add(rows); err != nil {
				return nil, err
			}
		}
	case "comment.published":
		for _, id := range []*uuid.UUID{payload.PostAuthorAgentID, payload.ParentAuthorAgentID} {
			if id != nil && *id != actor {
				set[*id] = true
			}
		}
	case "follow.created":
		if payload.TargetAgentID != nil && *payload.TargetAgentID != actor {
			set[*payload.TargetAgentID] = true
		}
	case "follow.removed":
		// Unfollowing is recorded in history but does not notify the target.
	case "reaction.updated":
		if payload.PostAuthorAgentID != nil && *payload.PostAuthorAgentID != actor {
			set[*payload.PostAuthorAgentID] = true
		}
	case "community.role.granted", "community.role.revoked":
		if payload.TargetAgentID != nil && *payload.TargetAgentID != actor {
			set[*payload.TargetAgentID] = true
		}
	case "post.hidden", "post.unhidden", "post.appeal.upheld", "post.appeal.overturned":
		if payload.PostAuthorAgentID != nil &&
			(payload.ActorAgentID == nil || *payload.PostAuthorAgentID != *payload.ActorAgentID) {
			set[*payload.PostAuthorAgentID] = true
		}
	}
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	if len(out) > maxFanout {
		out = out[:maxFanout]
	}
	return out, nil
}

func addNotification(ctx context.Context, tx pgx.Tx, recipient, eventID uuid.UUID, now time.Time) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notifications
		WHERE recipient_agent_id = $1 AND event_id = $2)`, recipient, eventID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_inboxes (agent_id) VALUES ($1)
		ON CONFLICT (agent_id) DO NOTHING`, recipient); err != nil {
		return err
	}
	var lastSeq int64
	if err := tx.QueryRow(ctx, `SELECT last_seq FROM agent_inboxes
		WHERE agent_id = $1 FOR UPDATE`, recipient).Scan(&lastSeq); err != nil {
		return err
	}
	next := lastSeq + 1
	if _, err := tx.Exec(ctx, `UPDATE agent_inboxes SET last_seq = $2 WHERE agent_id = $1`, recipient, next); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO notifications
		(recipient_agent_id, inbox_seq, event_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, recipient, next, eventID, now, now.Add(NotificationRetentionDays*24*time.Hour))
	return err
}

func (s *Store) recordOutboxFailure(ctx context.Context, eventID uuid.UUID, attempts int, cause error) error {
	nextAttempts := attempts + 1
	backoff := time.Duration(1<<minInt(nextAttempts, 8)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	if nextAttempts >= 5 {
		_, err := s.Pool.Exec(ctx, `UPDATE outbox_events
			SET attempts = $2, last_error = $3, processed_at = $4,
			    locked_at = NULL, locked_by = NULL
			WHERE id = $1`, eventID, nextAttempts, "dead_letter: "+message, s.now())
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE outbox_events
		SET attempts = $2, last_error = $3, next_attempt_at = $4,
		    locked_at = NULL, locked_by = NULL
		WHERE id = $1`, eventID, nextAttempts, message, s.now().Add(backoff))
	return err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

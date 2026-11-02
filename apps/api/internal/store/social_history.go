package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ActivityRecord struct {
	ID             uuid.UUID  `json:"id"`
	Type           string     `json:"type"`
	ActorAgentID   *uuid.UUID `json:"actor_agent_id"`
	SubjectAgentID *uuid.UUID `json:"subject_agent_id"`
	ObjectType     string     `json:"object_type"`
	ObjectID       uuid.UUID  `json:"object_id"`
	ConversationID *uuid.UUID `json:"conversation_id"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

const activityRecordColumns = `id, event_type, actor_agent_id, subject_agent_id,
	object_type, object_id, conversation_id, occurred_at`

func scanActivityRecords(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]ActivityRecord, error) {
	var records []ActivityRecord
	for rows.Next() {
		var record ActivityRecord
		if err := rows.Scan(&record.ID, &record.Type, &record.ActorAgentID, &record.SubjectAgentID,
			&record.ObjectType, &record.ObjectID, &record.ConversationID, &record.OccurredAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) AgentHistory(ctx context.Context, agentID uuid.UUID, cursor string, limit int) ([]ActivityRecord, string, bool, error) {
	limit = clampLimit(limit)
	cursorTime, cursorID, err := decodePublicCursor(cursor, "history")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+activityRecordColumns+`
		FROM activity_events
		WHERE (actor_agent_id = $1 OR subject_agent_id = $1)
		  AND ($2::timestamptz IS NULL OR (occurred_at, id) < ($2::timestamptz, $3::uuid))
		ORDER BY occurred_at DESC, id DESC LIMIT $4`, agentID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	records, err := scanActivityRecords(rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	next := ""
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		next = encodePublicCursor("history", &last.OccurredAt, last.ID)
	}
	return records, next, hasMore, nil
}

func (s *Store) PublicRelationships(ctx context.Context, agentID uuid.UUID, cursor string, limit int) ([]ActivityRecord, string, bool, error) {
	limit = clampLimit(limit)
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agents
		WHERE id = $1 AND status IN ('active', 'suspended'))`, agentID).Scan(&exists); err != nil {
		return nil, "", false, err
	}
	if !exists {
		return nil, "", false, ErrNotFound
	}
	cursorTime, cursorID, err := decodePublicCursor(cursor, "relationship")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT e.id, e.event_type, e.actor_agent_id, e.subject_agent_id,
		e.object_type, e.object_id, e.conversation_id, e.occurred_at
		FROM activity_events e
		LEFT JOIN comments c ON e.event_type = 'comment.published' AND c.id = e.object_id
		LEFT JOIN posts pc ON pc.id = c.post_id
		LEFT JOIN posts pr ON e.event_type = 'reaction.updated' AND pr.id = e.object_id
		LEFT JOIN follows cf ON e.event_type = 'follow.created'
		  AND cf.follower_agent_id = e.actor_agent_id AND cf.followee_agent_id = e.subject_agent_id
		WHERE (e.actor_agent_id = $1 OR e.subject_agent_id = $1)
		  AND e.event_type IN ('follow.created', 'comment.published', 'reaction.updated')
		  AND (e.subject_agent_id IS NULL OR e.actor_agent_id IS NULL OR e.actor_agent_id <> e.subject_agent_id)
		  AND (e.event_type <> 'follow.created' OR cf.follower_agent_id IS NOT NULL)
		  AND (e.event_type <> 'follow.created' OR e.id = (
		       SELECT e2.id FROM activity_events e2
		       WHERE e2.event_type = 'follow.created'
		         AND e2.actor_agent_id = e.actor_agent_id
		         AND e2.subject_agent_id = e.subject_agent_id
		       ORDER BY e2.occurred_at DESC, e2.id DESC LIMIT 1))
		  AND (e.event_type <> 'comment.published' OR
		       (c.status = 'published' AND pc.status = 'published' AND pc.community_hidden_at IS NULL))
		  AND (e.event_type <> 'reaction.updated' OR
		       (pr.status = 'published' AND pr.community_hidden_at IS NULL))
		  AND ($2::timestamptz IS NULL OR (e.occurred_at, e.id) < ($2::timestamptz, $3::uuid))
		ORDER BY e.occurred_at DESC, e.id DESC LIMIT $4`, agentID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	records, err := scanActivityRecords(rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	next := ""
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		next = encodePublicCursor("relationship", &last.OccurredAt, last.ID)
	}
	return records, next, hasMore, nil
}

func (s *Store) CommunityChronicle(ctx context.Context, communityID uuid.UUID, cursor string, limit int) ([]ActivityRecord, string, bool, error) {
	limit = clampLimit(limit)
	var active bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM communities
		WHERE id = $1 AND status = 'active')`, communityID).Scan(&active); err != nil {
		return nil, "", false, err
	}
	if !active {
		return nil, "", false, ErrNotFound
	}
	cursorTime, cursorID, err := decodePublicCursor(cursor, "chronicle")
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id, event_type, actor_agent_id, subject_agent_id,
		object_type, object_id, conversation_id, occurred_at
		FROM (
			SELECT id, event_type, actor_agent_id, subject_agent_id,
				'community'::text AS object_type, community_id AS object_id,
				NULL::uuid AS conversation_id, created_at AS occurred_at
			FROM community_governance_events WHERE community_id = $1
			UNION ALL
			SELECT e.id, e.event_type, e.actor_agent_id, e.subject_agent_id,
				e.object_type, e.object_id, e.conversation_id, e.occurred_at
			FROM activity_events e JOIN posts p ON p.id = e.object_id
			WHERE e.event_type = 'post.published' AND p.community_id = $1
			  AND p.status = 'published' AND p.community_hidden_at IS NULL
			UNION ALL
			SELECT e.id, e.event_type, e.actor_agent_id, e.subject_agent_id,
				e.object_type, e.object_id, e.conversation_id, e.occurred_at
			FROM activity_events e
			JOIN comments c ON c.id = e.object_id
			JOIN posts p ON p.id = c.post_id
			WHERE e.event_type = 'comment.published' AND p.community_id = $1
			  AND p.status = 'published' AND p.community_hidden_at IS NULL
			  AND c.status = 'published'
		) events
		WHERE ($2::timestamptz IS NULL OR (occurred_at, id) < ($2::timestamptz, $3::uuid))
		ORDER BY occurred_at DESC, id DESC LIMIT $4`, communityID, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	records, err := scanActivityRecords(rows)
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	next := ""
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		next = encodePublicCursor("chronicle", &last.OccurredAt, last.ID)
	}
	return records, next, hasMore, nil
}

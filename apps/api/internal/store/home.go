package store

import (
	"context"

	"github.com/google/uuid"
)

type HomeData struct {
	Agent       Agent
	UnreadCount int
	LastSeq     int64
	Quota       map[string]int
}

func (s *Store) GetAgentHome(ctx context.Context, agentID uuid.UUID, postLimit, commentLimit int) (HomeData, error) {
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return HomeData{}, err
	}
	var home HomeData
	home.Agent = agent
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications
		WHERE recipient_agent_id = $1 AND acknowledged_at IS NULL AND expires_at > $2`,
		agentID, s.now()).Scan(&home.UnreadCount); err != nil {
		return HomeData{}, err
	}
	if err := s.Pool.QueryRow(ctx, `SELECT COALESCE(
		(SELECT last_seq FROM agent_inboxes WHERE agent_id = $1), 0)`, agentID).
		Scan(&home.LastSeq); err != nil {
		return HomeData{}, err
	}
	used := map[string]int{}
	rows, err := s.Pool.Query(ctx, `SELECT operation, used FROM daily_write_counters
		WHERE agent_id = $1 AND day_utc = $2`, agentID, s.now().UTC().Format("2006-01-02"))
	if err != nil {
		return HomeData{}, err
	}
	for rows.Next() {
		var operation string
		var count int
		if err := rows.Scan(&operation, &count); err != nil {
			rows.Close()
			return HomeData{}, err
		}
		used[operation] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HomeData{}, err
	}
	rows.Close()
	remaining := func(limit, count int) int {
		left := limit - count
		if left < 0 {
			return 0
		}
		return left
	}
	home.Quota = map[string]int{
		"posts":    remaining(postLimit, used["post.create"]),
		"comments": remaining(commentLimit, used["comment.create"]),
	}
	return home, nil
}

func (s *Store) AcknowledgeEvents(ctx context.Context, agentID uuid.UUID, eventIDs []uuid.UUID) (int, error) {
	if len(eventIDs) == 0 || len(eventIDs) > 50 {
		return 0, ErrInvalid
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE notifications SET acknowledged_at = $3
		WHERE recipient_agent_id = $1 AND event_id = ANY($2::uuid[])
		  AND acknowledged_at IS NULL`, agentID, eventIDs, s.now())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

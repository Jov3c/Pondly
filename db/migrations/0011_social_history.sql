-- Phase 05: subject side of social facts, history/relationship indexes.

ALTER TABLE activity_events
  ADD COLUMN IF NOT EXISTS subject_agent_id uuid REFERENCES agents(id);
CREATE INDEX IF NOT EXISTS idx_activity_events_actor_time
  ON activity_events (actor_agent_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_activity_events_subject_time
  ON activity_events (subject_agent_id, occurred_at DESC, id DESC);

UPDATE agent_credentials
SET scopes = array_append(scopes, 'history:read')
WHERE NOT ('history:read' = ANY(scopes));
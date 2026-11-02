-- Phase 05: append-only community governance log for role and rule changes.

CREATE TABLE IF NOT EXISTS community_governance_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id uuid NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
    event_type varchar(32) NOT NULL CHECK (event_type IN (
        'community.created', 'moderator.granted', 'moderator.revoked', 'rules.updated'
    )),
    actor_agent_id uuid NOT NULL REFERENCES agents(id),
    subject_agent_id uuid REFERENCES agents(id),
    rules_snapshot text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((event_type IN ('community.created', 'rules.updated')) = (rules_snapshot IS NOT NULL)),
    CHECK ((event_type IN ('moderator.granted', 'moderator.revoked')) = (subject_agent_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_community_governance_events_public
  ON community_governance_events (community_id, created_at, id);

INSERT INTO community_governance_events
  (community_id, event_type, actor_agent_id, subject_agent_id, rules_snapshot, created_at)
SELECT c.id, 'community.created', c.creator_agent_id, NULL, c.rules, c.created_at
FROM communities c
WHERE NOT EXISTS (
    SELECT 1 FROM community_governance_events e
    WHERE e.community_id = c.id AND e.event_type = 'community.created'
);
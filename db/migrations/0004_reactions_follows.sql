-- Phase 02: social relationships and reactions. Database constraints are the
-- authority for duplicate votes and self-follow, not application pre-checks.

CREATE TABLE IF NOT EXISTS reactions (
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    post_id uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    type varchar(16) NOT NULL CHECK (type = 'like'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, post_id)
);
CREATE INDEX IF NOT EXISTS idx_reactions_post ON reactions (post_id);

CREATE TABLE IF NOT EXISTS follows (
    follower_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    followee_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_agent_id, followee_agent_id),
    CHECK (follower_agent_id <> followee_agent_id)
);
CREATE INDEX IF NOT EXISTS idx_follows_followee ON follows (followee_agent_id);

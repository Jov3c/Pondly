-- Phase 05 P05-01b: community post hides and author appeals.

ALTER TABLE posts ADD COLUMN IF NOT EXISTS community_hidden_at timestamptz;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS community_hidden_by_agent_id uuid REFERENCES agents(id);
ALTER TABLE posts ADD COLUMN IF NOT EXISTS community_hide_reason text;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_community_hide_consistent') THEN
        ALTER TABLE posts ADD CONSTRAINT posts_community_hide_consistent CHECK (
            (community_hidden_at IS NULL AND community_hidden_by_agent_id IS NULL
             AND community_hide_reason IS NULL)
            OR
            (community_hidden_at IS NOT NULL AND community_hidden_by_agent_id IS NOT NULL
             AND community_hide_reason IS NOT NULL
             AND char_length(community_hide_reason) BETWEEN 10 AND 500)
        );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_posts_community_hidden
  ON posts (community_id, community_hidden_at) WHERE community_hidden_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS post_appeals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    community_id uuid NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
    appellant_agent_id uuid NOT NULL REFERENCES agents(id),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 10 AND 1000),
    state varchar(16) NOT NULL DEFAULT 'open'
      CHECK (state IN ('open', 'upheld', 'overturned')),
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    resolved_by_kind varchar(16) CHECK (resolved_by_kind IN ('admin', 'community')),
    resolved_by_admin_id uuid REFERENCES admin_accounts(id),
    resolved_by_agent_id uuid REFERENCES agents(id),
    resolution_reason text,
    CHECK (resolution_reason IS NULL OR char_length(resolution_reason) BETWEEN 10 AND 1000),
    CHECK (
        (state = 'open' AND resolved_at IS NULL AND resolved_by_kind IS NULL
         AND resolved_by_admin_id IS NULL AND resolved_by_agent_id IS NULL
         AND resolution_reason IS NULL)
        OR
        (state <> 'open' AND resolved_at IS NOT NULL AND resolved_by_kind IS NOT NULL
         AND resolution_reason IS NOT NULL
         AND ((resolved_by_kind = 'admin' AND resolved_by_admin_id IS NOT NULL
               AND resolved_by_agent_id IS NULL)
              OR (resolved_by_kind = 'community' AND resolved_by_agent_id IS NOT NULL
                  AND resolved_by_admin_id IS NULL)))
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_post_appeals_open
  ON post_appeals (post_id) WHERE state = 'open';
CREATE INDEX IF NOT EXISTS idx_post_appeals_queue
  ON post_appeals (state, created_at, id);

UPDATE agent_credentials
SET scopes = array_append(scopes, 'appeals:write')
WHERE NOT ('appeals:write' = ANY(scopes));
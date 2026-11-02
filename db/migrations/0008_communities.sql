-- Phase 02 Beta: minimal communities without complex governance or federation.

CREATE TABLE IF NOT EXISTS communities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug varchar(64) NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{2,64}$'),
    name varchar(80) NOT NULL CHECK (char_length(name) BETWEEN 2 AND 80),
    description varchar(500),
    rules text NOT NULL CHECK (char_length(rules) BETWEEN 10 AND 4000),
    creator_agent_id uuid NOT NULL REFERENCES agents(id),
    status varchar(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_communities_public
  ON communities (created_at, id) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS community_members (
    community_id uuid NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role varchar(16) NOT NULL DEFAULT 'member'
      CHECK (role IN ('owner', 'moderator', 'member')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_community_members_agent
  ON community_members (agent_id);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_community_fk') THEN
        ALTER TABLE posts ADD CONSTRAINT posts_community_fk
          FOREIGN KEY (community_id) REFERENCES communities(id);
    END IF;
END $$;

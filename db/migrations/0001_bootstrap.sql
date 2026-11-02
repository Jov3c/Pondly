-- Pondly Phase 01 schema proposal. Do not run as API runtime role.
-- PostgreSQL 15+. Run in a dedicated database using a migration role.
BEGIN;

CREATE TABLE IF NOT EXISTS owners (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    handle varchar(64) NOT NULL UNIQUE,
    status varchar(16) NOT NULL DEFAULT 'unclaimed'
      CHECK (status IN ('unclaimed', 'active', 'suspended', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    scopes text[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    revoked_at timestamptz,
    CHECK (octet_length(token_hash) = 32)
);
CREATE INDEX IF NOT EXISTS idx_agent_credentials_live ON agent_credentials(agent_id)
  WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS agent_ownerships (
    agent_id uuid PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    claimed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS posts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_agent_id uuid NOT NULL REFERENCES agents(id),
    post_type varchar(16) NOT NULL CHECK (post_type IN ('moment','discussion')),
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 8000),
    status varchar(24) NOT NULL DEFAULT 'pending_review'
      CHECK (status IN ('pending_review', 'published','rejected','deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_posts_feed ON posts (published_at DESC, id DESC)
  WHERE status = 'published';

CREATE TABLE IF NOT EXISTS outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type varchar(80) NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0)
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox_events(created_at, id)
  WHERE processed_at IS NULL;

CREATE TABLE IF NOT EXISTS idempotency_records (
    principal_id uuid NOT NULL,
    operation varchar(120) NOT NULL,
    key varchar(128) NOT NULL,
    request_hash bytea NOT NULL,
    response_status integer,
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(principal_id, operation, key),
    CHECK (octet_length(request_hash) = 32)
);

CREATE TABLE IF NOT EXISTS daily_write_counters (
    agent_id uuid NOT NULL REFERENCES agents(id),
    day_utc date NOT NULL,
    operation varchar(80) NOT NULL,
    used integer NOT NULL DEFAULT 0 CHECK (used >= 0),
    PRIMARY KEY (agent_id, day_utc, operation)
);

COMMIT;

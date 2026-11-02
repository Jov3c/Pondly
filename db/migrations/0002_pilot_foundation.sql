-- Pondly Pilot foundation. 0001 is frozen; this migration is additive.
-- Apply with a migration role, never the API runtime role.

ALTER TABLE owners ADD COLUMN IF NOT EXISTS email_normalized text;
ALTER TABLE owners ADD COLUMN IF NOT EXISTS email_hash bytea;
ALTER TABLE owners ADD COLUMN IF NOT EXISTS status varchar(16) NOT NULL DEFAULT 'active';
CREATE UNIQUE INDEX IF NOT EXISTS idx_owners_email_normalized
  ON owners (email_normalized) WHERE email_normalized IS NOT NULL;

CREATE TABLE IF NOT EXISTS owner_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    session_hash bytea NOT NULL UNIQUE CHECK (octet_length(session_hash) = 32),
    csrf_key bytea NOT NULL CHECK (octet_length(csrf_key) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    step_up_at timestamptz,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_owner_sessions_live
  ON owner_sessions (owner_id) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS owner_otp_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email_normalized text NOT NULL,
    owner_id uuid REFERENCES owners(id) ON DELETE SET NULL,
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    purpose varchar(16) NOT NULL DEFAULT 'sign_in'
      CHECK (purpose IN ('sign_in', 'step_up')),
    session_id uuid REFERENCES owner_sessions(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_owner_otp_recent
  ON owner_otp_challenges (email_normalized, created_at DESC);

CREATE TABLE IF NOT EXISTS enrollment_tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    ticket_hash bytea NOT NULL UNIQUE CHECK (octet_length(ticket_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    redeemed_agent_id uuid REFERENCES agents(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_enrollment_tickets_owner
  ON enrollment_tickets (owner_id, expires_at DESC);

CREATE TABLE IF NOT EXISTS claim_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    challenge_hash bytea NOT NULL UNIQUE CHECK (octet_length(challenge_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_claim_challenges_agent
  ON claim_challenges (agent_id, expires_at DESC);

CREATE TABLE IF NOT EXISTS agent_recovery_tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
    ticket_hash bytea NOT NULL UNIQUE CHECK (octet_length(ticket_hash) = 32),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recovery_tickets_agent
  ON agent_recovery_tickets (agent_id, expires_at DESC);

CREATE TABLE IF NOT EXISTS agent_profiles (
    agent_id uuid PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    display_name text,
    bio text,
    model_label text,
    framework_label text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE agents ALTER COLUMN handle TYPE varchar(32);
ALTER TABLE agents ADD CONSTRAINT agents_handle_format
  CHECK (handle ~ '^[a-z][a-z0-9_-]{1,31}$');
ALTER TABLE agents ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE posts ADD COLUMN IF NOT EXISTS title text;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS community_id uuid;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_posts_published_feed
  ON posts (published_at DESC, id DESC) WHERE status = 'published';

CREATE TABLE IF NOT EXISTS comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    author_agent_id uuid NOT NULL REFERENCES agents(id),
    parent_id uuid REFERENCES comments(id),
    depth integer NOT NULL DEFAULT 0 CHECK (depth BETWEEN 0 AND 4),
    content text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 4000),
    status varchar(24) NOT NULL DEFAULT 'pending_review'
      CHECK (status IN ('pending_review', 'published', 'rejected', 'deleted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_comments_post_order
  ON comments (post_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_comments_published_post
  ON comments (post_id, created_at, id) WHERE status = 'published';

CREATE TABLE IF NOT EXISTS agent_inboxes (
    agent_id uuid PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    last_seq bigint NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    lowest_retained_seq bigint NOT NULL DEFAULT 1 CHECK (lowest_retained_seq >= 1)
);

CREATE TABLE IF NOT EXISTS activity_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type varchar(80) NOT NULL,
    actor_agent_id uuid REFERENCES agents(id),
    object_type varchar(16) NOT NULL,
    object_id uuid NOT NULL,
    conversation_id uuid,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_activity_events_time
  ON activity_events (occurred_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    inbox_seq bigint NOT NULL CHECK (inbox_seq >= 1),
    event_id uuid NOT NULL REFERENCES activity_events(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    UNIQUE (recipient_agent_id, inbox_seq),
    UNIQUE (recipient_agent_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_notifications_retention
  ON notifications (recipient_agent_id, expires_at);

ALTER TABLE idempotency_records
  ADD COLUMN IF NOT EXISTS secret_ciphertext bytea;
ALTER TABLE idempotency_records
  ADD COLUMN IF NOT EXISTS secret_nonce bytea;
ALTER TABLE idempotency_records
  ADD COLUMN IF NOT EXISTS secret_expires_at timestamptz;
ALTER TABLE idempotency_records
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS next_attempt_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS last_error text;
ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS locked_at timestamptz;
ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS locked_by text;
CREATE INDEX IF NOT EXISTS idx_outbox_ready
  ON outbox_events (next_attempt_at, created_at) WHERE processed_at IS NULL;

CREATE TABLE IF NOT EXISTS admin_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    totp_secret text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id uuid NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    session_hash bytea NOT NULL UNIQUE CHECK (octet_length(session_hash) = 32),
    csrf_key bytea NOT NULL CHECK (octet_length(csrf_key) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_live
  ON admin_sessions (admin_id) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS moderation_actions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type varchar(16) NOT NULL CHECK (target_type IN ('post', 'comment')),
    target_id uuid NOT NULL,
    actor_kind varchar(16) NOT NULL CHECK (actor_kind IN ('system', 'admin')),
    actor_id uuid,
    decision varchar(16) NOT NULL CHECK (decision IN ('approve', 'reject', 'suspend', 'dismiss', 'resolve')),
    reason text NOT NULL,
    request_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_moderation_target
  ON moderation_actions (target_type, target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_kind varchar(16) NOT NULL,
    actor_id uuid,
    action varchar(80) NOT NULL,
    target_type varchar(32),
    target_id uuid,
    request_id text,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_time
  ON audit_logs (created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS pilot_publish_allowlist (
    agent_id uuid PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    created_by_admin_id uuid REFERENCES admin_accounts(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

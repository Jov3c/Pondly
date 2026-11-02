-- Phase 02: private safety reports. Reports never create public social posts.

CREATE TABLE IF NOT EXISTS reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_kind varchar(16) NOT NULL CHECK (reporter_kind IN ('agent', 'owner')),
    reporter_agent_id uuid REFERENCES agents(id) ON DELETE CASCADE,
    reporter_owner_id uuid REFERENCES owners(id) ON DELETE CASCADE,
    target_type varchar(16) NOT NULL CHECK (target_type IN ('post', 'comment', 'agent', 'community')),
    target_id uuid NOT NULL,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 10 AND 1000),
    state varchar(16) NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'resolved', 'dismissed')),
    resolved_by_admin_id uuid REFERENCES admin_accounts(id),
    resolution_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (reporter_kind = 'agent' AND reporter_agent_id IS NOT NULL AND reporter_owner_id IS NULL)
        OR (reporter_kind = 'owner' AND reporter_owner_id IS NOT NULL AND reporter_agent_id IS NULL)
    )
);
CREATE INDEX IF NOT EXISTS idx_reports_open_queue ON reports (state, created_at, id);
CREATE INDEX IF NOT EXISTS idx_reports_target ON reports (target_type, target_id);

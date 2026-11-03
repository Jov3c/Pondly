CREATE TABLE IF NOT EXISTS agent_api_request_counters (
    agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    day_utc date NOT NULL,
    used integer NOT NULL DEFAULT 0 CHECK (used >= 0),
    PRIMARY KEY (agent_id, day_utc)
);

-- One owner owns exactly one Agent.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_ownerships_owner_unique
  ON agent_ownerships (owner_id);

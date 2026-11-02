-- Phase 02: Owner-managed API request budget for claimed Agents.

CREATE TABLE IF NOT EXISTS owner_agent_policies (
    agent_id uuid PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    api_request_budget_per_day integer NOT NULL DEFAULT 1000
      CHECK (api_request_budget_per_day BETWEEN 0 AND 100000),
    updated_at timestamptz NOT NULL DEFAULT now()
);

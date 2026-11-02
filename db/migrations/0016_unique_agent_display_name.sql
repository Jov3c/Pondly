CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_profiles_display_name_unique
  ON agent_profiles (lower(btrim(display_name)))
  WHERE display_name IS NOT NULL;

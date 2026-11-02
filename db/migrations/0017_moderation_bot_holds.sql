ALTER TABLE moderation_actions
  DROP CONSTRAINT IF EXISTS moderation_actions_decision_check;
ALTER TABLE moderation_actions
  ADD CONSTRAINT moderation_actions_decision_check
  CHECK (decision IN ('approve', 'reject', 'suspend', 'dismiss', 'resolve', 'hold'));

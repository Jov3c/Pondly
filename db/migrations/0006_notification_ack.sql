-- Phase 02: per-recipient acknowledgement for the Beta home/ack surface.
-- Cursor-based event delivery remains independent and does not require ack.

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS acknowledged_at timestamptz;
CREATE INDEX IF NOT EXISTS idx_notifications_unread
  ON notifications (recipient_agent_id, acknowledged_at)
  WHERE acknowledged_at IS NULL;

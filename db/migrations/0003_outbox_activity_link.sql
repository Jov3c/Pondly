-- Link each activity event to the Outbox row that produced it so a worker
-- replay cannot create a second event or duplicate notifications.

ALTER TABLE activity_events
  ADD COLUMN IF NOT EXISTS outbox_event_id uuid UNIQUE
  REFERENCES outbox_events(id) ON DELETE CASCADE;

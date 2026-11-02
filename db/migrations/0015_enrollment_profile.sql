ALTER TABLE enrollment_tickets ADD COLUMN IF NOT EXISTS display_name varchar(80);
ALTER TABLE enrollment_tickets ADD COLUMN IF NOT EXISTS avatar_key varchar(32);

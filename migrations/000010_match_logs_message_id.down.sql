DROP INDEX IF EXISTS idx_match_logs_message_id;
ALTER TABLE match_logs DROP COLUMN IF EXISTS message_id;

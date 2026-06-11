DROP INDEX IF EXISTS idx_match_preparation_message_id;
ALTER TABLE match_preparation DROP COLUMN IF EXISTS message_id;

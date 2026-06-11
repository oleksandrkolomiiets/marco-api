DROP INDEX IF EXISTS idx_messages_user_id_created_at_active;

CREATE INDEX idx_messages_user_id_created_at ON messages (user_id, created_at DESC);

ALTER TABLE messages DROP COLUMN deleted_at;

ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMPTZ NULL;

CREATE INDEX idx_messages_user_id_created_at_active
  ON messages (user_id, created_at)
  WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_messages_user_id_created_at;

COMMENT ON COLUMN messages.deleted_at IS 'Soft-delete timestamp; NULL means visible. Used for hide-message and retry flows.';

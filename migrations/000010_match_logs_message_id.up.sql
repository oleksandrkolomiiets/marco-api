-- Link a match log back to the assistant chat message that prompted it so the
-- chat UI can render the "Log this match" tag as "Logged" across app restarts.
-- Nullable so manually-created match logs (Profile / Matches page) still work.
ALTER TABLE match_logs ADD COLUMN message_id UUID REFERENCES messages(id) ON DELETE SET NULL;
CREATE INDEX idx_match_logs_message_id ON match_logs (message_id) WHERE message_id IS NOT NULL;

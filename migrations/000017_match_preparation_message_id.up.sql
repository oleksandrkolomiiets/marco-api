-- Link a match preparation back to the assistant chat message that prompted
-- it so the chat UI can render the "Set up prep" tag as "Prep ready" across
-- app restarts. Mirrors the match_logs.message_id pattern from 000010.
-- Nullable so preps created from the Match Prep tab (no chat origin) still work.
ALTER TABLE match_preparation
    ADD COLUMN message_id UUID REFERENCES messages(id) ON DELETE SET NULL;

CREATE INDEX idx_match_preparation_message_id
    ON match_preparation (message_id)
    WHERE message_id IS NOT NULL;

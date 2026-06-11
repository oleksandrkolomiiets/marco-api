-- played_at is set when the player explicitly marks a prep as done, so a
-- readiness can be "done" before its scheduled time (early start) or stay
-- "upcoming" after it (rescheduled). The plain scheduled_at < now() rule
-- used to imply done-ness, but that conflated calendar slippage with the
-- player's own completion signal.
ALTER TABLE match_readiness
    ADD COLUMN played_at TIMESTAMPTZ;

CREATE INDEX idx_match_readiness_user_id_played_at
    ON match_readiness (user_id, played_at DESC)
    WHERE played_at IS NOT NULL;

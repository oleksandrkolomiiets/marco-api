ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_dominant_hand_check;
ALTER TABLE users
    ADD CONSTRAINT users_dominant_hand_check
    CHECK (dominant_hand IN ('left', 'right', 'both'));

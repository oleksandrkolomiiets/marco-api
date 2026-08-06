-- "Connected devices" needs something device-shaped to list, and refresh
-- tokens are the wrong grain: /auth/refresh rotates them, deleting the row and
-- inserting a new one, so a phone that has been signed in for a month looks
-- like a device that first appeared fifteen minutes ago. A session is the
-- thing that persists across rotation.
CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- All nullable: a client that sends no device headers still gets a
    -- session, it just shows up unnamed rather than being refused.
    device_name  VARCHAR(120),
    platform     VARCHAR(60),
    app_version  VARCHAR(40),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Bumped on every token refresh, which is the only regular heartbeat we
    -- have without writing a row on every single API call.
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Kept rather than deleted so a revoked device disappears from the list
    -- without taking its history with it.
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX idx_sessions_user_last_seen ON sessions (user_id, last_seen_at DESC);

ALTER TABLE refresh_tokens ADD COLUMN session_id UUID REFERENCES sessions(id) ON DELETE CASCADE;

-- Backfill: every token already out there belongs to a device someone is
-- using, so give each one a session rather than signing everybody out. Reusing
-- the token's own id as the session id makes the match deterministic in two
-- plain statements instead of a correlated insert.
INSERT INTO sessions (id, user_id, created_at, last_seen_at)
SELECT id, user_id, created_at, created_at FROM refresh_tokens;

UPDATE refresh_tokens SET session_id = id WHERE session_id IS NULL;

-- Only safe once the backfill above has run.
ALTER TABLE refresh_tokens ALTER COLUMN session_id SET NOT NULL;

CREATE INDEX idx_refresh_tokens_session_id ON refresh_tokens (session_id);

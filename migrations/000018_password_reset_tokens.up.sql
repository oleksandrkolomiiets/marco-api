-- Password reset. A user who forgets their password had no way back into the
-- account: the app's "Forgot password?" link had no handler and there was no
-- endpoint behind it.
--
-- code_hash holds a bcrypt hash of a 6-digit code, not a SHA-256 one like
-- refresh_tokens. Six digits is only a million possibilities, so a fast digest
-- would be exhaustible from a database dump in seconds; bcrypt makes that cost
-- real for a value that only has to survive fifteen minutes. It also means the
-- code is NOT unique and NOT lookup-able on its own — bcrypt salts every hash,
-- so a reset must arrive with the email and be verified against that user's row.
-- That is deliberate: a bare six-digit code is guessable across a large enough
-- user base, and scoping the lookup to one account removes that entirely.
CREATE TABLE password_reset_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash   VARCHAR(255) NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    -- Set when the code is spent. Kept rather than deleted so the row also
    -- serves as the "don't send another email for 60s" record.
    consumed_at TIMESTAMPTZ,
    -- Wrong guesses against this code. The handler burns the token at 5.
    attempts    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every read is "the newest live token for this user", so order the index the
-- way that query scans it.
CREATE INDEX idx_password_reset_tokens_user_created
    ON password_reset_tokens (user_id, created_at DESC);

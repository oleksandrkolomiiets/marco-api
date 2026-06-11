ALTER TABLE users ADD COLUMN injury_notes TEXT;

CREATE TABLE goals (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    description TEXT         NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'achieved', 'archived')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TRIGGER goals_set_updated_at
    BEFORE UPDATE ON goals
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_goals_user_id_status ON goals (user_id, status);

CREATE TABLE match_logs (
    id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    played     BOOLEAN      NOT NULL DEFAULT true,
    result     VARCHAR(20),
    feeling    VARCHAR(50),
    note       TEXT,
    played_on  DATE         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_match_logs_user_id_played_on ON match_logs (user_id, played_on DESC);

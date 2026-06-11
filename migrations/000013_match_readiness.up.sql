CREATE TABLE match_readiness (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    match_log_id  UUID         REFERENCES match_logs(id) ON DELETE SET NULL,
    scheduled_at  TIMESTAMPTZ  NOT NULL,
    opponents     TEXT[]       NOT NULL DEFAULT '{}',
    partner_name  VARCHAR(100),
    court         VARCHAR(50),
    note          TEXT,
    plan_grade    VARCHAR(20)  CHECK (plan_grade IN ('worked', 'missed')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TRIGGER match_readiness_set_updated_at
    BEFORE UPDATE ON match_readiness
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_match_readiness_user_id_scheduled_at
    ON match_readiness (user_id, scheduled_at DESC);

CREATE INDEX idx_match_readiness_match_log_id
    ON match_readiness (match_log_id)
    WHERE match_log_id IS NOT NULL;

CREATE TABLE match_readiness_drills (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    match_readiness_id   UUID         NOT NULL REFERENCES match_readiness(id) ON DELETE CASCADE,
    position             INT          NOT NULL,
    title                VARCHAR(200) NOT NULL,
    duration_seconds     INT          NOT NULL CHECK (duration_seconds >= 0),
    completed            BOOLEAN      NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_match_readiness_drills_readiness_position
    ON match_readiness_drills (match_readiness_id, position);

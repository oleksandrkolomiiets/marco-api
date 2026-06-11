DROP INDEX IF EXISTS idx_match_readiness_user_id_played_at;

ALTER TABLE match_readiness
    DROP COLUMN IF EXISTS played_at;

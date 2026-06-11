-- Rename match_readiness → match_preparation across tables, columns, indexes,
-- the FK column, the plan-grade check constraint, and the updated_at trigger.
-- The product surface treats this as "match preparation"; the old name was a
-- carry-over from an earlier naming pass.

ALTER TABLE match_readiness RENAME TO match_preparation;
ALTER TABLE match_readiness_drills RENAME TO match_preparation_drills;

ALTER TABLE match_preparation_drills
    RENAME COLUMN match_readiness_id TO match_preparation_id;

ALTER INDEX idx_match_readiness_user_id_scheduled_at
    RENAME TO idx_match_preparation_user_id_scheduled_at;
ALTER INDEX idx_match_readiness_match_log_id
    RENAME TO idx_match_preparation_match_log_id;
ALTER INDEX idx_match_readiness_user_id_played_at
    RENAME TO idx_match_preparation_user_id_played_at;
ALTER INDEX idx_match_readiness_drills_readiness_position
    RENAME TO idx_match_preparation_drills_preparation_position;

ALTER TABLE match_preparation
    RENAME CONSTRAINT match_readiness_plan_grade_check
    TO match_preparation_plan_grade_check;

ALTER TRIGGER match_readiness_set_updated_at ON match_preparation
    RENAME TO match_preparation_set_updated_at;

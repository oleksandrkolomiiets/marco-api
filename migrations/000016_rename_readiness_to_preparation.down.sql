ALTER TRIGGER match_preparation_set_updated_at ON match_preparation
    RENAME TO match_readiness_set_updated_at;

ALTER TABLE match_preparation
    RENAME CONSTRAINT match_preparation_plan_grade_check
    TO match_readiness_plan_grade_check;

ALTER INDEX idx_match_preparation_drills_preparation_position
    RENAME TO idx_match_readiness_drills_readiness_position;
ALTER INDEX idx_match_preparation_user_id_played_at
    RENAME TO idx_match_readiness_user_id_played_at;
ALTER INDEX idx_match_preparation_match_log_id
    RENAME TO idx_match_readiness_match_log_id;
ALTER INDEX idx_match_preparation_user_id_scheduled_at
    RENAME TO idx_match_readiness_user_id_scheduled_at;

ALTER TABLE match_preparation_drills
    RENAME COLUMN match_preparation_id TO match_readiness_id;

ALTER TABLE match_preparation_drills RENAME TO match_readiness_drills;
ALTER TABLE match_preparation RENAME TO match_readiness;

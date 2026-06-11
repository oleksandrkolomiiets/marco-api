UPDATE match_readiness SET plan_grade = NULL WHERE plan_grade = 'mixed';

ALTER TABLE match_readiness
    DROP CONSTRAINT match_readiness_plan_grade_check;

ALTER TABLE match_readiness
    ADD CONSTRAINT match_readiness_plan_grade_check
    CHECK (plan_grade IN ('worked', 'missed'));

DROP TABLE IF EXISTS drills;
DROP TABLE IF EXISTS lesson_cues;

DROP TRIGGER IF EXISTS lessons_set_updated_at ON lessons;

ALTER TABLE lessons
    DROP COLUMN IF EXISTS tagline,
    DROP COLUMN IF EXISTS focus,
    DROP COLUMN IF EXISTS common_mistake_pct,
    DROP COLUMN IF EXISTS common_mistake_text,
    DROP COLUMN IF EXISTS updated_at,
    ADD COLUMN cue_points     JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN common_mistake TEXT,
    ADD COLUMN drill          TEXT;

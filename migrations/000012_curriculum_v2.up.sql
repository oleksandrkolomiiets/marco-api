-- Curriculum v2: tagline, focus, structured common-mistake percentage,
-- and normalised lesson_cues + drills tables. Replaces the legacy
-- cue_points / common_mistake / drill columns on lessons.

ALTER TABLE lessons
    DROP COLUMN cue_points,
    DROP COLUMN common_mistake,
    DROP COLUMN drill,
    ADD COLUMN tagline             TEXT,
    ADD COLUMN focus               TEXT,
    ADD COLUMN common_mistake_pct  SMALLINT CHECK (common_mistake_pct BETWEEN 0 AND 100),
    ADD COLUMN common_mistake_text TEXT,
    ADD COLUMN updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TRIGGER lessons_set_updated_at
    BEFORE UPDATE ON lessons
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE lesson_cues (
    id                UUID     PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id         UUID     NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    timestamp_seconds SMALLINT NOT NULL CHECK (timestamp_seconds IN (3, 7, 11)),
    cue_text          TEXT     NOT NULL,
    sort_order        SMALLINT NOT NULL,
    UNIQUE (lesson_id, timestamp_seconds)
);

CREATE INDEX idx_lesson_cues_lesson_sort ON lesson_cues (lesson_id, sort_order);

CREATE TABLE drills (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id        UUID         NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    name             VARCHAR(120) NOT NULL,
    duration_minutes SMALLINT     NOT NULL CHECK (duration_minutes > 0),
    is_recommended   BOOLEAN      NOT NULL,
    description      TEXT         NOT NULL,
    UNIQUE (lesson_id)
);

-- Restoring the 3/7/11 constraint means any cue stored at a real timestamp
-- would violate it, so those rows have to go back to the mock positions first.
-- Ordering by sort_order keeps the cues in the order the lesson lists them.
UPDATE lesson_cues c
SET timestamp_seconds = m.mock
FROM (
    SELECT id, (ARRAY[3, 7, 11])[
        LEAST(ROW_NUMBER() OVER (PARTITION BY lesson_id ORDER BY sort_order), 3)
    ] AS mock
    FROM lesson_cues
) m
WHERE c.id = m.id AND c.timestamp_seconds <> m.mock;

-- A lesson with more than three cues cannot fit the old shape at all: the
-- fourth onwards would collide on UNIQUE (lesson_id, timestamp_seconds).
DELETE FROM lesson_cues c
USING (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY lesson_id ORDER BY sort_order) AS n
    FROM lesson_cues
) r
WHERE c.id = r.id AND r.n > 3;

ALTER TABLE lesson_cues DROP CONSTRAINT lesson_cues_timestamp_seconds_check;

ALTER TABLE lesson_cues
    ADD CONSTRAINT lesson_cues_timestamp_seconds_check
    CHECK (timestamp_seconds IN (3, 7, 11));

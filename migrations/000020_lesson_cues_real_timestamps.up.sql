-- lesson_cues.timestamp_seconds was CHECK (timestamp_seconds IN (3, 7, 11)),
-- which is the design mock written into the schema: every cue in the seeded
-- curriculum sits at 0:03, 0:07 or 0:11 because nothing else could be stored.
-- No lesson has a video_url either, so the app was showing three identical
-- timestamps as positions in a clip that does not exist.
--
-- The cue text is real coaching content and stays. This only frees the column
-- to hold a real position once lessons have something to play, and keeps the
-- guard that a timestamp cannot be negative.
ALTER TABLE lesson_cues DROP CONSTRAINT lesson_cues_timestamp_seconds_check;

ALTER TABLE lesson_cues
    ADD CONSTRAINT lesson_cues_timestamp_seconds_check
    CHECK (timestamp_seconds >= 0);

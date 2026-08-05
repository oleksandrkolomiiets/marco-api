-- ============================================================================
-- QA HARNESS FIXTURES — internal/marco/system_prompt_v1.md test cases
--
-- !! DESTRUCTIVE !! This script truncates the users table (CASCADE clears
-- goals, match_logs, messages, user_lesson_progress, refresh_tokens). Run it
-- only against a local dev database. Never against staging or production.
--
-- Lessons: the 5 beginner slugs the QA cases depend on are seeded here with
-- ON CONFLICT (slug) DO NOTHING so they survive a re-run AND don't overwrite
-- a richer seed_lessons.sql run that already populated them with full
-- cue_points / drill / etc. The titles MUST match seed_lessons.sql exactly —
-- Marco's prompt requires verbatim titles from the curriculum.
--
-- Idempotent: re-running wipes prior user state and re-seeds cleanly.
-- ============================================================================

INSERT INTO lessons (slug, title, level, order_index, is_free, published)
VALUES
  ('ready-position', 'The Ready Position',         'beginner', 1, true, true),
  ('forehand-drive', 'The Forehand Drive',         'beginner', 2, true, true),
  ('backhand-drive', 'The Backhand Drive',         'beginner', 3, false, true),
  ('serve-basics',   'The Padel Serve',            'beginner', 4, true, true),
  ('volley-intro',   'Introduction to the Volley', 'beginner', 5, true, true)
ON CONFLICT (slug) DO NOTHING;

TRUNCATE TABLE users CASCADE;

-- ---------------------------------------------------------------------------
-- joost_beginner — right-handed beginner, right side, active backhand-lob goal.
-- Used by A1, A3, B1, C2, C3, D1, D2, E1, F1, G1, G2, G3, C4.
-- ---------------------------------------------------------------------------
INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, goal, injury_notes, plan)
VALUES (
  '11111111-1111-1111-1111-111111111111',
  'qa-fixture-joost',
  'joost@qa.local',
  'Joost',
  'beginner',
  'right',
  'right',
  '2x/week',
  'improve net game',
  NULL,
  'coach'
);

INSERT INTO goals (user_id, description, status, created_at) VALUES
  ('11111111-1111-1111-1111-111111111111', 'improve backhand lob',   'active', '2026-05-01 10:00:00+00'),
  ('11111111-1111-1111-1111-111111111111', 'win more points at net', 'active', '2026-05-02 10:00:00+00');

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '11111111-1111-1111-1111-111111111111', id, 'mastered', '2026-05-10 10:00:00+00'
FROM lessons WHERE slug = 'volley-intro';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '11111111-1111-1111-1111-111111111111', id, 'learned', '2026-05-11 10:00:00+00'
FROM lessons WHERE slug = 'forehand-drive';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '11111111-1111-1111-1111-111111111111', id, 'viewed', '2026-05-12 10:00:00+00'
FROM lessons WHERE slug = 'ready-position';

INSERT INTO match_logs (user_id, played, result, feeling, note, played_on) VALUES
  ('11111111-1111-1111-1111-111111111111', true, 'lost', 'frustrated', 'kept missing volleys at the net', DATE '2026-05-14');

-- Match prep fixtures for the I-group (match prep via chat). Joost gets two
-- upcoming preps so the harness covers both weekday-reference flows:
--   * one on the next Thursday at 20:00 (I1: "Adjust Thursday's prep")
--   * one tomorrow at 19:30 (I4: "Add a bandeja drill to tomorrow's prep")
-- Scheduled_at is computed relative to NOW() so the rows remain "upcoming"
-- regardless of when the harness runs. UUIDs are pinned so the upcoming_
-- preparations[] block in Marco's context contains predictable ids.
INSERT INTO match_preparation (id, user_id, scheduled_at, opponents, partner_name, court, note) VALUES
  (
    '99999999-9999-9999-9999-999999999901',
    '11111111-1111-1111-1111-111111111111',
    -- "Next Thursday", forced at least two days out. The naive next-Thursday
    -- lands on tomorrow when today is Wednesday, colliding with the Clara prep
    -- below: I1 ("Adjust Thursday's prep") then has two Thursday rows to choose
    -- between and I4 ("tomorrow's prep") has two candidates, so one day in
    -- seven neither case could be exercised as written.
    date_trunc('day', NOW())
      + (((4 - EXTRACT(DOW FROM NOW())::int + 6) % 7) + 1
         + CASE WHEN ((4 - EXTRACT(DOW FROM NOW())::int + 6) % 7) + 1 = 1 THEN 7 ELSE 0 END)
        * INTERVAL '1 day'
      + INTERVAL '20 hours',
    ARRAY['Lucia', 'Pablo']::text[],
    'Tom',
    'Court 2',
    'Lost 5–7 last time. Bandeja was the gap.'
  ),
  (
    '99999999-9999-9999-9999-999999999902',
    '11111111-1111-1111-1111-111111111111',
    date_trunc('day', NOW()) + INTERVAL '1 day' + INTERVAL '19 hours 30 minutes',
    ARRAY['Clara']::text[],
    NULL,
    NULL,
    NULL
  );

-- ---------------------------------------------------------------------------
-- sandra_intermediate — right hand, left side, intermediate.
-- Mastered bandeja_basics; completed vibora intro. Used by A2, E2.
-- Lesson slugs net_positioning_basics / volley_fundamentals from the spec do
-- not exist in seed_lessons.sql yet — we approximate with the closest seeded
-- lessons (volley-intro, ready-position) so progress rows can be inserted.
-- Marco's prompt sees the slug list; once real lessons are seeded with the
-- spec names, swap them in here.
-- ---------------------------------------------------------------------------
INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, goal, injury_notes, plan)
VALUES (
  '22222222-2222-2222-2222-222222222222',
  'qa-fixture-sandra',
  'sandra@qa.local',
  'Sandra',
  'intermediate',
  'right',
  'left',
  '3x/week',
  'sharpen overheads',
  NULL,
  'coach'
);

INSERT INTO goals (user_id, description, status, created_at) VALUES
  ('22222222-2222-2222-2222-222222222222', 'get better at the net', 'active', '2026-05-01 10:00:00+00');

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '22222222-2222-2222-2222-222222222222', id, 'mastered', '2026-05-08 10:00:00+00'
FROM lessons WHERE slug = 'volley-intro';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '22222222-2222-2222-2222-222222222222', id, 'mastered', '2026-05-09 10:00:00+00'
FROM lessons WHERE slug = 'ready-position';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '22222222-2222-2222-2222-222222222222', id, 'learned', '2026-05-12 10:00:00+00'
FROM lessons WHERE slug = 'forehand-drive';

-- ---------------------------------------------------------------------------
-- lena_intermediate_left — right hand, left side, intermediate. Just won.
-- Used by B2.
-- ---------------------------------------------------------------------------
INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, goal, injury_notes, plan)
VALUES (
  '33333333-3333-3333-3333-333333333333',
  'qa-fixture-lena',
  'lena@qa.local',
  'Lena',
  'intermediate',
  'right',
  'left',
  '2x/week',
  'consistency in matches',
  NULL,
  'coach'
);

INSERT INTO match_logs (user_id, played, result, feeling, note, played_on) VALUES
  ('33333333-3333-3333-3333-333333333333', true, 'won', 'great', 'finally got the lob working', DATE '2026-05-14');

-- ---------------------------------------------------------------------------
-- anonymous_user — minimal profile. No goals, no matches, no progress.
-- Used by C1, B3, F2 (boundary cases where rich context would muddy the test).
-- ---------------------------------------------------------------------------
INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, goal, injury_notes, plan)
VALUES (
  '44444444-4444-4444-4444-444444444444',
  'qa-fixture-anon',
  'anon@qa.local',
  'Anon',
  'beginner',
  NULL,
  NULL,
  NULL,
  NULL,
  NULL,
  'coach'
);

-- ============================================================================
-- QA HARNESS FIXTURES — internal/marco/system_prompt_v1.md test cases
--
-- !! DESTRUCTIVE !! This script truncates the users table (CASCADE clears
-- goals, match_logs, messages, user_lesson_progress, refresh_tokens). Run it
-- only against a local dev database. Never against staging or production.
--
-- Lessons: this file used to insert five of its own beginner lessons
-- (ready-position, forehand-drive, backhand-drive, serve-basics,
-- volley-intro) so the progress rows below had something to attach to. Those
-- slugs came from an old seed_lessons.sql. The real curriculum replaced it
-- with different slugs, so ON CONFLICT (slug) DO NOTHING never merged
-- anything — it just added five extra lessons on every run.
--
-- The damage was quiet and cumulative: 35 lessons instead of 30, two
-- different lessons claiming "beginner #1" (and #2 … #5) because
-- (level, order_index) collided, phantom lessons with no tagline, no cues and
-- no common mistake showing up in the app, and every lesson-count stat —
-- progress, mastery rate, the Half Mastery achievement — computed over five
-- lessons that were never part of the curriculum.
--
-- The progress rows now point at real curriculum lessons, which is what the
-- note below sandra_intermediate always asked for. This file no longer
-- creates lessons; `make seed` owns the curriculum.
--
-- Idempotent: re-running wipes prior user state and re-seeds cleanly.
-- ============================================================================

-- Fail loudly rather than silently attaching no progress. Every progress
-- insert below is INSERT … SELECT, so without the curriculum they quietly
-- insert nothing and the QA cases run against users whose lesson history is
-- empty — which is exactly how the old fixtures rotted unnoticed.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM lessons WHERE slug = 'volley-basics-block-push') THEN
        RAISE EXCEPTION
            'curriculum not seeded: run `make seed` before `make qa`';
    END IF;
END $$;

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
FROM lessons WHERE slug = 'volley-basics-block-push';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '11111111-1111-1111-1111-111111111111', id, 'learned', '2026-05-11 10:00:00+00'
FROM lessons WHERE slug = 'forehand-groundstroke';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '11111111-1111-1111-1111-111111111111', id, 'viewed', '2026-05-12 10:00:00+00'
FROM lessons WHERE slug = 'ready-position-split-step';

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
-- E2 checks Marco does not re-teach what she has mastered, so her mastered
-- set is the beginner net and positioning work: asked "how do I get better at
-- the net", the honest answer is an intermediate lesson such as
-- net-domination-volley-patterns, not the volley basics she already owns.
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
FROM lessons WHERE slug = 'volley-basics-block-push';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '22222222-2222-2222-2222-222222222222', id, 'mastered', '2026-05-09 10:00:00+00'
FROM lessons WHERE slug = 'ready-position-split-step';

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
SELECT '22222222-2222-2222-2222-222222222222', id, 'learned', '2026-05-12 10:00:00+00'
FROM lessons WHERE slug = 'forehand-groundstroke';

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

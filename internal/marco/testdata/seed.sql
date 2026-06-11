-- Deterministic fixture for marco.Assembler tests.
-- Assumes a freshly-migrated DB; uses ON CONFLICT for idempotency on lessons
-- so the test can re-seed without TRUNCATE on the lessons table.

INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, goal, injury_notes, plan)
VALUES (
  '11111111-1111-1111-1111-111111111111',
  'google-test-joost',
  'joost@test.local',
  'Joost',
  'beginner',
  'right',
  'right',
  '2x/week',
  'improve net game',
  NULL,
  'coach'
);

INSERT INTO lessons (id, slug, title, level, order_index, is_free, published)
VALUES
  ('22222222-2222-2222-2222-222222222201', 'volley-intro',    'Volley Intro',    'beginner', 101, true, true),
  ('22222222-2222-2222-2222-222222222202', 'forehand-drive',  'Forehand Drive',  'beginner', 102, true, true),
  ('22222222-2222-2222-2222-222222222203', 'ready-position',  'Ready Position',  'beginner', 103, true, true)
ON CONFLICT (slug) DO UPDATE SET title = EXCLUDED.title;

INSERT INTO goals (user_id, description, status, created_at)
VALUES
  ('11111111-1111-1111-1111-111111111111', 'improve net positioning', 'active', '2026-05-01 10:00:00+00'),
  ('11111111-1111-1111-1111-111111111111', 'win more points at net',  'active', '2026-05-02 10:00:00+00');

INSERT INTO user_lesson_progress (user_id, lesson_id, status, updated_at)
VALUES
  ('11111111-1111-1111-1111-111111111111',
   (SELECT id FROM lessons WHERE slug = 'volley-intro'),    'mastered', '2026-05-10 10:00:00+00'),
  ('11111111-1111-1111-1111-111111111111',
   (SELECT id FROM lessons WHERE slug = 'forehand-drive'),  'learned',  '2026-05-11 10:00:00+00'),
  ('11111111-1111-1111-1111-111111111111',
   (SELECT id FROM lessons WHERE slug = 'ready-position'),  'viewed',   '2026-05-12 10:00:00+00');

INSERT INTO match_logs (user_id, played, result, feeling, note, played_on)
VALUES
  ('11111111-1111-1111-1111-111111111111', true, 'lost', 'frustrated', 'kept missing volleys at the net', DATE '2026-05-14');

INSERT INTO match_preparation (id, user_id, scheduled_at, opponents, partner_name, court, note)
VALUES (
  '55555555-5555-5555-5555-555555555501',
  '11111111-1111-1111-1111-111111111111',
  -- Pinned far into the future so the loader's NOW() - 24h filter always
  -- includes the row and the golden file stays deterministic.
  TIMESTAMPTZ '2099-01-15 18:00:00+00',
  ARRAY['Lucia','Pablo']::text[],
  'Tom',
  'Court 2',
  'Right knee a bit stiff — go easy on lateral drills.'
);

INSERT INTO messages (user_id, role, content, created_at)
VALUES
  ('11111111-1111-1111-1111-111111111111', 'user',      'Hi Marco, can you help me?',           '2026-05-15 08:00:00+00'),
  ('11111111-1111-1111-1111-111111111111', 'assistant', 'Vamos! What do you want to work on?',  '2026-05-15 08:00:05+00');

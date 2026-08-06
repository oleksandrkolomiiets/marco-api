-- Rules-exam content. Idempotent: nukes existing exam_questions (cascades to
-- exam_options and exam_attempt_answers) and re-inserts the full set, so it's
-- safe to re-run after editing.
--
-- SOURCE OF TRUTH: "RULES OF PADEL", International Padel Federation, review of
-- application 01.01.2026 — https://www.padelfip.com/wp-content/uploads/2025/12/FIP_Rules-of-Padel.pdf
-- The numbered rules in that edition are:
--   1 Score in a Game        7 Serve Fault              13 Point Lost
--   2 Times                  8 Return of Serve          14 Correct Return
--   3 Position of Players    9 Repeat/"Let" and "Net"   15 Point Won
--   4 Choice of Sides        10 Repetition/"Let" Point  16 Authorized Out-of-Court Play
--   5 Changes of Sides       11 Interference            17 Change of Balls
--   6 The Serve              12 Ball in Play
--
-- Every `explanation` below cites a rule and sub-clause from that document and
-- was checked against it question by question. They were previously wrong on
-- twelve of the twenty: serving cited Rule 5 (Changes of Sides) rather than
-- Rule 6, changing ends cited Rule 17 (Change of Balls) rather than Rule 5, the
-- double bounce and the out-of-court chase cited Rule 11 (Interference), and so
-- on. The answers were all correct; only the references were not. If you edit a
-- question, re-check its citation against the current FIP edition and update
-- the list above if the numbering has moved.

TRUNCATE exam_questions RESTART IDENTITY CASCADE;

WITH q AS (
    INSERT INTO exam_questions (slug, order_index, category, prompt, explanation) VALUES
    ('serve-direction', 1, 'Service · direction',
     'You serve from your RIGHT box. The ball clears the net but lands straight across, in the opponents'' LEFT box. Call?',
     'FIP · Rule 6.5 · the serve must travel diagonally into the receiver''s service box.'),
    ('contact-height', 2, 'Service · contact height',
     'Your bounce comes up high. You strike the ball cleanly at chest height into the correct box. Call?',
     'FIP · Rule 6.4 · the ball must be struck at or below waist height, with one foot on the ground.'),
    ('foot-fault', 3, 'Service · foot fault',
     'You step on the service line as you make contact. The ball lands cleanly in the correct box. Call?',
     'FIP · Rule 6.3 · the server may not touch the service line or the centre line; Rule 7.1(a) makes that a fault.'),
    ('bounce-first', 4, 'Service · bounce first',
     'You toss the ball and strike it out of the air. It lands cleanly in the correct service box. Call?',
     'FIP · Rule 6.2 · the server must bounce the ball on the ground before striking it.'),
    ('fence-after-bounce', 5, 'Service · fence contact',
     'Your serve bounces inside the correct service box, then hits the side mesh fence. Call?',
     'FIP · Rule 7.1(e) · a serve landing in the box then touching the metallic fence before the second bounce is a fault.'),
    ('back-glass-live', 6, 'Service · back glass',
     'Your serve bounces inside the correct service box, then hits the back glass. Call?',
     'FIP · Rule 12.3 · only the metallic fence faults a serve (Rule 7.1e) — off the back glass the ball stays in play.'),
    ('net-cord-let', 7, 'Service · let',
     'Your serve clips the net cord, drops into the correct service box, no fence contact. Call?',
     'FIP · Rule 9.1(a) · a serve that touches the net and lands in the correct box is a "net" — replayed, no penalty.'),
    ('receiver-volleys', 8, 'Return · serve volley',
     'Your serve flies toward the box. The receiver steps in and volleys it out of the air before it bounces. Call?',
     'FIP · Rule 8.1 · the receiver must let the serve bounce; Rule 8.5 gives the point to the server if they hit it first.'),
    ('own-side-wall', 9, 'Wall play · own side',
     'Their shot bounces on your court, hits your back glass, comes back up. You smash it over the net. Call?',
     'FIP · Rule 12.3 · after bouncing on your floor the ball stays live off your own walls; Rule 14.1(b) makes the return good.'),
    ('opp-side-wall', 10, 'Wall play · opp side',
     'Your shot clears the net and hits the opponents'' back glass BEFORE bouncing on their floor. Call?',
     'FIP · Rule 13.1(g) · a ball reaching the opponents'' wall without bouncing on their floor first loses the point.'),
    ('body-contact', 11, 'Rally · body contact',
     'Your partner is at the net. The opponents'' shot strikes your partner on the shoulder before bouncing. Call?',
     'FIP · Rule 13.1(k) · a ball hitting any part of a player or their kit other than the racket loses the point.'),
    ('net-touch', 12, 'Rally · net touch',
     'You win a delicate drop volley. Following through, your racket grazes the top of the net. Call?',
     'FIP · Rule 13.1(a) · player, racket or clothing touching the net, posts or cable while the ball is in play loses the point.'),
    ('double-bounce', 13, 'Rally · double bounce',
     'Defending deep, you let the ball bounce, then it bounces a SECOND time before you can return it. Call?',
     'FIP · Rule 13.1(c) · the ball bouncing a second time before it is returned loses the point.'),
    ('out-of-court-chase', 14, 'Rally · out of court',
     'Their lob bounces in your court, then sails over the side glass and out of the cage. You sprint out the door and return it. Call?',
     'FIP · Rule 16 · leaving the court to play the ball is legal only where the court is built with the safety area for it.'),
    ('ceiling-clip', 15, 'Rally · ceiling',
     'Indoor club, low ceiling. Your lob clips the ceiling on the way over the net. Call?',
     'FIP · Rule 14.1(d) · the ceiling only counts as a good return after the ball has bounced in the opponents'' court.'),
    ('reaching-over-net', 16, 'Rally · over the net',
     'Their drop shot has wicked backspin and starts heading back over the net before you can hit it. You reach across, over their side, and tap it. Call?',
     'FIP · Rule 14.1(g) · a ball that bounced in and spun back may be played over the net, touching neither net nor their court.'),
    ('ball-in-then-out', 17, 'Rally · ball flies out',
     'Their smash bounces hard in your court, then ricochets over the back fence and out. Nobody chases it. Call?',
     'FIP · Rule 13.1(d)-(e) · it bounced in, so the shot was good — leaving the court afterwards does not undo that.'),
    ('side-changes', 18, 'Match · side changes',
     'Score is 3–2 in the first set. This game ends. Do the teams change sides of the court?',
     'FIP · Rule 5.1 · sides change after the 1st, 3rd and every subsequent odd game of the set.'),
    ('golden-point-side', 19, 'Scoring · golden point',
     'Game hits deuce in a golden-point format. Who decides which side the receiver takes the deciding point from?',
     'FIP · Rule 1, Golden Point · the receiving pair picks the side for the deciding point and may not swap positions.'),
    ('racket-cord-snap', 20, 'Rally · racket cord snap',
     'Mid-rally, your safety cord snaps and your racket flies onto the court. Call?',
     'FIP · Rule 13.1(r) · breaking your safety cord or dropping your racket loses the point immediately.')
    RETURNING id, slug
)
INSERT INTO exam_options (question_id, order_index, text, is_correct)
SELECT q.id, opt.order_index, opt.text, opt.is_correct
FROM q
JOIN (VALUES
    -- Q1
    ('serve-direction', 1, 'Good serve — point in play', FALSE),
    ('serve-direction', 2, 'Fault — service must be diagonal', TRUE),
    ('serve-direction', 3, 'Let — replay the serve', FALSE),
    ('serve-direction', 4, 'Good only if receiver doesn''t move', FALSE),
    -- Q2
    ('contact-height', 1, 'Good serve', FALSE),
    ('contact-height', 2, 'Fault — contact must be at or below the waist', TRUE),
    ('contact-height', 3, 'Let', FALSE),
    ('contact-height', 4, 'Good if the receiver doesn''t object', FALSE),
    -- Q3
    ('foot-fault', 1, 'Good — the line is part of the box', FALSE),
    ('foot-fault', 2, 'Fault — feet must stay behind the line until contact', TRUE),
    ('foot-fault', 3, 'Let', FALSE),
    ('foot-fault', 4, 'Warning only, no fault', FALSE),
    -- Q4
    ('bounce-first', 1, 'Good serve', FALSE),
    ('bounce-first', 2, 'Fault — the ball must bounce on the floor before you strike it', TRUE),
    ('bounce-first', 3, 'Let', FALSE),
    ('bounce-first', 4, 'Good only on a second serve', FALSE),
    -- Q5
    ('fence-after-bounce', 1, 'Good — the bounce was in', FALSE),
    ('fence-after-bounce', 2, 'Fault — on the serve, the ball must not touch the side fence', TRUE),
    ('fence-after-bounce', 3, 'Let', FALSE),
    ('fence-after-bounce', 4, 'Point for you', FALSE),
    -- Q6
    ('back-glass-live', 1, 'Good — the return is live, opponent must play it', TRUE),
    ('back-glass-live', 2, 'Fault — ball touched the wall', FALSE),
    ('back-glass-live', 3, 'Let', FALSE),
    ('back-glass-live', 4, 'Point for you automatically', FALSE),
    -- Q7
    ('net-cord-let', 1, 'Good — point in play', FALSE),
    ('net-cord-let', 2, 'Fault', FALSE),
    ('net-cord-let', 3, 'Let — replay the serve, no penalty', TRUE),
    ('net-cord-let', 4, 'Point for opponents', FALSE),
    -- Q8
    ('receiver-volleys', 1, 'Good — return was clean', FALSE),
    ('receiver-volleys', 2, 'Point for the serving team — receiver must let it bounce', TRUE),
    ('receiver-volleys', 3, 'Replay — receiver wasn''t ready', FALSE),
    ('receiver-volleys', 4, 'Fault on the receiver, second serve', FALSE),
    -- Q9
    ('own-side-wall', 1, 'Good — walls on your side are in play', TRUE),
    ('own-side-wall', 2, 'Fault — wall contact ends the point', FALSE),
    ('own-side-wall', 3, 'Let', FALSE),
    ('own-side-wall', 4, 'Point for opponents', FALSE),
    -- Q10
    ('opp-side-wall', 1, 'Good — wall sent it back', FALSE),
    ('opp-side-wall', 2, 'Out — on the opponent''s side, the ball must bounce first', TRUE),
    ('opp-side-wall', 3, 'Let', FALSE),
    ('opp-side-wall', 4, 'Replay', FALSE),
    -- Q11
    ('body-contact', 1, 'Good — body contact doesn''t count', FALSE),
    ('body-contact', 2, 'Point for the opponents — body contact loses the point', TRUE),
    ('body-contact', 3, 'Replay if it was accidental', FALSE),
    ('body-contact', 4, 'Let', FALSE),
    -- Q12
    ('net-touch', 1, 'Good — clean winner', FALSE),
    ('net-touch', 2, 'Point for opponents — touching the net during play loses the point', TRUE),
    ('net-touch', 3, 'Good if contact was accidental', FALSE),
    ('net-touch', 4, 'Replay', FALSE),
    -- Q13
    ('double-bounce', 1, 'Good if you got it back', FALSE),
    ('double-bounce', 2, 'Point for opponents — only one bounce allowed', TRUE),
    ('double-bounce', 3, 'Replay', FALSE),
    ('double-bounce', 4, 'Let', FALSE),
    -- Q14
    ('out-of-court-chase', 1, 'Good — out-of-court play is allowed once the ball has bounced inside', TRUE),
    ('out-of-court-chase', 2, 'Out — once the ball leaves the cage, the point is dead', FALSE),
    ('out-of-court-chase', 3, 'Point for opponents — you can''t leave the court', FALSE),
    ('out-of-court-chase', 4, 'Let', FALSE),
    -- Q15
    ('ceiling-clip', 1, 'Good — the lob crossed', FALSE),
    ('ceiling-clip', 2, 'Point for opponents — ceiling contact loses the point', TRUE),
    ('ceiling-clip', 3, 'Replay if it was the first lob', FALSE),
    ('ceiling-clip', 4, 'Let', FALSE),
    -- Q16
    ('reaching-over-net', 1, 'Good — the ball was returning to your side anyway', TRUE),
    ('reaching-over-net', 2, 'Point for opponents — you may not reach over the net', FALSE),
    ('reaching-over-net', 3, 'Replay', FALSE),
    ('reaching-over-net', 4, 'Let', FALSE),
    -- Q17
    ('ball-in-then-out', 1, 'Point for you', FALSE),
    ('ball-in-then-out', 2, 'Point for the opponents — bounced in, you didn''t return it', TRUE),
    ('ball-in-then-out', 3, 'Out — the ball left the court', FALSE),
    ('ball-in-then-out', 4, 'Let', FALSE),
    -- Q18
    ('side-changes', 1, 'Yes — teams switch after every odd total games played', TRUE),
    ('side-changes', 2, 'Yes, but only after even-numbered games', FALSE),
    ('side-changes', 3, 'No — only at the end of each set', FALSE),
    ('side-changes', 4, 'Only if a team asks for the switch', FALSE),
    -- Q19
    ('golden-point-side', 1, 'The receiving pair chooses the side', TRUE),
    ('golden-point-side', 2, 'The server''s choice', FALSE),
    ('golden-point-side', 3, 'Coin toss', FALSE),
    ('golden-point-side', 4, 'The umpire', FALSE),
    -- Q20
    ('racket-cord-snap', 1, 'Good if you grab it before the ball returns', FALSE),
    ('racket-cord-snap', 2, 'Point for opponents — losing your racket loses the point immediately', TRUE),
    ('racket-cord-snap', 3, 'Replay — equipment malfunction', FALSE),
    ('racket-cord-snap', 4, 'Warning only on first occurrence', FALSE)
) AS opt(slug, order_index, text, is_correct)
ON q.slug = opt.slug;

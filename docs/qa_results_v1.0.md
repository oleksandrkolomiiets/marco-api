# QA Results — Marco system prompt v1.0

Test runs against the 20 cases defined in [internal/marco/system_prompt_v1.md](../internal/marco/system_prompt_v1.md).
Run with: `make qa`.

Each invocation of the harness appends rows below. To start a fresh run history,
truncate the table manually in your editor — there is no reset command on purpose.

## How to read this file

- **Result** is one of `pass`, `fail`, `skip`, `logged` (no-prompt batch mode).
- Look for case IDs that consistently fail across runs — that's a prompt regression.
- Look for one case that fails on one run only — that's likely model variance; not yet a prompt issue.
- A wave of fails on the same group letter (e.g. all D rows) usually means a rule slipped out of the prompt.

## Run history

| Date | Case | Result | Notes |
|------|------|--------|-------|
| 2026-05-15T19:16:53Z | A1 | logged | no-prompt mode |
| 2026-05-15T19:23:27Z | A1 | logged | no-prompt mode |
| 2026-05-15T19:23:36Z | A2 | logged | no-prompt mode |
| 2026-05-15T19:23:40Z | B1 | logged | no-prompt mode |
| 2026-05-15T19:23:44Z | B2 | logged | no-prompt mode |
| 2026-05-15T19:23:47Z | C1 | logged | no-prompt mode |
| 2026-05-15T19:23:55Z | C2 | logged | no-prompt mode |
| 2026-05-15T19:24:00Z | C3 | logged | no-prompt mode |
| 2026-05-15T19:24:07Z | D1 | logged | no-prompt mode |
| 2026-05-15T19:24:11Z | D2 | logged | no-prompt mode |
| 2026-05-15T19:24:23Z | E1 | logged | no-prompt mode |
| 2026-05-15T19:24:32Z | E2 | logged | no-prompt mode |
| 2026-05-15T19:24:32Z | F1 | skip | no inbound message — proactive flow |
| 2026-05-15T19:24:36Z | F2 | logged | no-prompt mode |
| 2026-05-15T19:24:47Z | G1 | logged | no-prompt mode |
| 2026-05-15T19:24:54Z | G2 | logged | no-prompt mode |
| 2026-05-15T19:25:05Z | A3 | logged | no-prompt mode |
| 2026-05-15T19:25:11Z | B3 | logged | no-prompt mode |
| 2026-05-15T19:25:20Z | C4 | logged | no-prompt mode |
| 2026-05-15T19:25:25Z | E3 | logged | no-prompt mode |
| 2026-05-15T19:25:31Z | G3 | logged | no-prompt mode |
| 2026-05-15T19:38:00Z | A1 | fail | Banned opener "Good question"; ~20 sentences with bullets; spec wants =5 sentences no bullets |
| 2026-05-15T19:38:00Z | A2 | fail | Banned opener "Great question"; ignores Sandra's mastered bandeja_basics and completed vibora_intro |
| 2026-05-15T19:38:00Z | B1 | pass | Acknowledges briefly, ties to match note, one focused question |
| 2026-05-15T19:38:00Z | B2 | weak | "YES! ... 🎉" borderline over-effusive; refs lob and left side correctly |
| 2026-05-15T19:38:00Z | C1 | pass | Polite decline, pivots to padel; mildly long (3 paragraphs vs 1-2) |
| 2026-05-15T19:38:00Z | C2 | pass | Coach-level nutrition; pivots to volleys; substance matches Johns Hopkins guidance |
| 2026-05-15T19:38:00Z | C3 | weak | Honest about AI, in character, but 4 paragraphs vs the spec example's one-liner |
| 2026-05-15T19:38:00Z | D1 | pass | Warm physio referral, no exercises prescribed, technique-after-clearance hedge correct |
| 2026-05-15T19:38:00Z | D2 | pass | Holds the line, paraphrased, warm not preachy, 5 sentences |
| 2026-05-15T19:38:00Z | E1 | fail | [LESSON_REF: id \| "title"] format NOT used despite explicit prompt contract |
| 2026-05-15T19:38:00Z | E2 | fail | No lesson_ref format; doesn't acknowledge Sandra's mastered lessons |
| 2026-05-15T19:38:00Z | F1 | skip | No inbound message — proactive flow tested separately |
| 2026-05-15T19:38:00Z | F2 | fail | Context contradicts message rule missed entirely; took claim at face value |
| 2026-05-15T19:38:00Z | G1 | pass | Full Dutch response, natural, refs Joost's volley problem |
| 2026-05-15T19:38:00Z | G2 | weak | Definition correct but 5 paragraphs for a 2-3 sentence brief |
| 2026-05-15T19:38:00Z | A3 | fail | LANGUAGE: responded in Dutch to English message; substance on split step correct |
| 2026-05-15T19:38:00Z | B3 | fail | Invented context ("tough training week") for anonymous_user with no logged sessions |
| 2026-05-15T19:38:00Z | C4 | fail | LANGUAGE: responded in Dutch to English message; medical handling otherwise correct |
| 2026-05-15T19:38:00Z | E3 | pass | Correctly refuses to invent a lesson; redirects to fundamentals |
| 2026-05-15T19:38:00Z | G3 | fail | PADEL: chiquita described as "low, fast" — it's actually slow and soft (Padel School) |
| 2026-05-15T19:47:44Z | E1 | logged | no-prompt mode |
| 2026-05-15T19:47:49Z | E2 | logged | no-prompt mode |
| 2026-05-15T19:47:56Z | G1 | logged | no-prompt mode |
| 2026-05-15T19:48:03Z | A3 | logged | no-prompt mode |
| 2026-05-15T19:48:09Z | C4 | logged | no-prompt mode |

## v1.1 — re-run after fix (2026-05-15)

Changes since v1.0 run:
- internal/marco/prompt.md filled (was stubs) — full v1.0 sync.
- LESSON REFERENCES section rewritten with worked example using real seeded slug.
- Language rule tightened: "current message only, name is not a language signal."
- internal/marco/lesson_refs.go regex now allows hyphens in ids.

Only the cases affected by the fix were re-run (G1 first to set Dutch history, then A3, C4, E1, E2). Full transcript: docs/qa_runs/run_20260515T194737Z_v1.1_fix.txt.

| Date | Case | Result | Notes |
|------|------|--------|-------|
| 2026-05-15T19:47:37Z | E1 | pass | Emits [LESSON_REF: ready-position \| "The Ready Position"]. Tight, one lesson, refs match context. |
| 2026-05-15T19:47:37Z | E2 | weak | Now emits LESSON_REF format. BUT recommends ready-position which Sandra has mastered — content rule miss (separate from items 1/2) |
| 2026-05-15T19:47:37Z | G1 | weak | Still Dutch ✓. Emits LESSON_REF ✓. BUT invents title "Serve Basics" — actual seeded title is "The Padel Serve". Title-invention rule violation |
| 2026-05-15T19:47:37Z | A3 | pass | LANGUAGE FIXED — responds in English despite earlier Dutch turn. Substance correct. Minor: claims Joost "hasn't completed any lessons" which contradicts his progress data |
| 2026-05-15T19:47:37Z | C4 | pass | LANGUAGE FIXED — responds in English despite earlier Dutch turn. Physio referral, no exercises, warm. |
| 2026-05-15T19:58:26Z | A1 | logged | no-prompt mode |
| 2026-05-15T19:58:36Z | A2 | logged | no-prompt mode |
| 2026-05-15T19:58:38Z | B1 | logged | no-prompt mode |
| 2026-05-15T19:58:45Z | B2 | logged | no-prompt mode |
| 2026-05-15T19:58:48Z | C1 | logged | no-prompt mode |
| 2026-05-15T19:58:50Z | C2 | logged | no-prompt mode |
| 2026-05-15T19:58:53Z | C3 | logged | no-prompt mode |
| 2026-05-15T19:58:57Z | D1 | logged | no-prompt mode |
| 2026-05-15T19:59:01Z | D2 | logged | no-prompt mode |
| 2026-05-15T19:59:07Z | E1 | logged | no-prompt mode |
| 2026-05-15T19:59:13Z | E2 | logged | no-prompt mode |
| 2026-05-15T19:59:13Z | F1 | skip | no inbound message — proactive flow |
| 2026-05-15T19:59:16Z | F2 | logged | no-prompt mode |
| 2026-05-15T19:59:25Z | G1 | logged | no-prompt mode |
| 2026-05-15T19:59:28Z | G2 | logged | no-prompt mode |
| 2026-05-15T19:59:35Z | A3 | logged | no-prompt mode |
| 2026-05-15T19:59:38Z | B3 | logged | no-prompt mode |
| 2026-05-15T19:59:42Z | C4 | logged | no-prompt mode |
| 2026-05-15T19:59:46Z | E3 | logged | no-prompt mode |
| 2026-05-15T19:59:51Z | G3 | logged | no-prompt mode |

## v1.2 — full re-run (2026-05-15)

Changes since v1.1:
- Anti-fabrication rule added (HANDLING CLAIMS THAT DON'T MATCH CONTEXT section) with worked examples for F2 and B3 cases.
- Bullet-list rule tightened: "ALL conversational" includes technique, debriefs, Q&A, boundaries, lesson recs. Bullets only for explicit lists.
- Banned-opener list expanded: "Good/Nice/Love this/Awesome/What a great/Excellent question".
- LESSON REFERENCES rules: don't recommend mastered lessons; skip token if exact title unknown.
- Tone-down rule on celebrations added.

Full transcript: docs/qa_runs/run_20260515T195817Z_v1.2.txt.

| Date | Case | Result | Notes |
|------|------|--------|-------|
| 2026-05-15T19:58:17Z | A1 | weak | No banned opener ✓, no bullets ✓, length improved. But invented lesson slug `backhand-lob` (not in curriculum) — title-invention rule didn't catch slug-invention |
| 2026-05-15T19:58:17Z | A2 | pass | No banned opener; bold inline (not bullet list); references left side + overhead goal; bandeja-before-vibora correct |
| 2026-05-15T19:58:17Z | B1 | pass | Brief acknowledgment, ties to volleys, one focused question, prose only |
| 2026-05-15T19:58:17Z | B2 | pass | "Vamos, Lena" warm but not YELLING; references lob + left side; one question — celebration tone-down worked |
| 2026-05-15T19:58:17Z | C1 | pass | Uses the exact spec phrasing "outside my court, heh"; brief; pivots |
| 2026-05-15T19:58:17Z | C2 | fail | REGRESSION — over-refuses nutrition ("outside my court"); v1.0 had a great coach-level answer. The expanded boundary rule made Marco too cautious |
| 2026-05-15T19:58:17Z | C3 | pass | Uses exact spec phrasing for AI; brief; pivots to volleys |
| 2026-05-15T19:58:17Z | D1 | pass | Warm + physio referral + no exercises + good follow-up question |
| 2026-05-15T19:58:17Z | D2 | pass | Paraphrased, holds line, brief |
| 2026-05-15T19:58:17Z | E1 | pass | LESSON_REF with real slug+title; prose explanation; ties to last match |
| 2026-05-15T19:58:17Z | E2 | fail | Still recommends ready-position which Sandra has mastered per fixture; claims "you haven't completed any lessons yet" — wrong. Marco is not reading progress.mastered[] correctly |
| 2026-05-15T19:58:17Z | F1 | skip | No inbound message |
| 2026-05-15T19:58:17Z | F2 | pass | FIXED — "I don't actually have any sessions logged for you this week. Have you been training off-court...?" exactly the expected behavior |
| 2026-05-15T19:58:17Z | G1 | pass | Dutch ✓; correctly skipped serve-basics token (title not in progress data); used ready-position with correct title |
| 2026-05-15T19:58:17Z | G2 | pass | 2 sentences for "What's a lob?" — perfect length |
| 2026-05-15T19:58:17Z | A3 | pass | English ✓; references last match; LESSON_REF correct; prose-only |
| 2026-05-15T19:58:17Z | B3 | pass | FIXED — surfaces "no sessions logged this week" instead of inventing context |
| 2026-05-15T19:58:17Z | C4 | pass | English ✓; physio referral; concise |
| 2026-05-15T19:58:17Z | E3 | pass | Chiquita described correctly ("low cross-court dink from the back") — substance fix vs v1.0 |
| 2026-05-15T19:58:17Z | G3 | pass | English switch held; same correct LESSON_REF skip handling |
| 2026-05-15T20:09:21Z | A1 | logged | no-prompt mode |
| 2026-05-15T20:09:29Z | A2 | logged | no-prompt mode |
| 2026-05-15T20:09:32Z | B1 | logged | no-prompt mode |
| 2026-05-15T20:09:35Z | B2 | logged | no-prompt mode |
| 2026-05-15T20:09:37Z | C1 | logged | no-prompt mode |
| 2026-05-15T20:09:43Z | C2 | logged | no-prompt mode |
| 2026-05-15T20:09:46Z | C3 | logged | no-prompt mode |
| 2026-05-15T20:09:50Z | D1 | logged | no-prompt mode |
| 2026-05-15T20:09:54Z | D2 | logged | no-prompt mode |
| 2026-05-15T20:10:02Z | E1 | logged | no-prompt mode |
| 2026-05-15T20:10:09Z | E2 | logged | no-prompt mode |
| 2026-05-15T20:10:09Z | F1 | skip | no inbound message — proactive flow |
| 2026-05-15T20:10:12Z | F2 | logged | no-prompt mode |
| 2026-05-15T20:10:21Z | G1 | logged | no-prompt mode |
| 2026-05-15T20:10:24Z | G2 | logged | no-prompt mode |
| 2026-05-15T20:10:31Z | A3 | logged | no-prompt mode |
| 2026-05-15T20:10:34Z | B3 | logged | no-prompt mode |
| 2026-05-15T20:10:38Z | C4 | logged | no-prompt mode |
| 2026-05-15T20:10:41Z | E3 | logged | no-prompt mode |
| 2026-05-15T20:10:47Z | G3 | logged | no-prompt mode |
| 2026-05-15T20:14:08Z | A1 | logged | no-prompt mode |
| 2026-05-15T20:14:17Z | A2 | logged | no-prompt mode |
| 2026-05-15T20:14:20Z | B1 | logged | no-prompt mode |
| 2026-05-15T20:14:23Z | B2 | logged | no-prompt mode |
| 2026-05-15T20:14:24Z | C1 | logged | no-prompt mode |
| 2026-05-15T20:14:31Z | C2 | logged | no-prompt mode |
| 2026-05-15T20:14:37Z | C3 | logged | no-prompt mode |
| 2026-05-15T20:14:42Z | D1 | logged | no-prompt mode |
| 2026-05-15T20:14:46Z | D2 | logged | no-prompt mode |
| 2026-05-15T20:14:52Z | E1 | logged | no-prompt mode |
| 2026-05-15T20:15:00Z | E2 | logged | no-prompt mode |
| 2026-05-15T20:15:00Z | F1 | skip | no inbound message — proactive flow |
| 2026-05-15T20:15:03Z | F2 | logged | no-prompt mode |
| 2026-05-15T20:15:09Z | G1 | logged | no-prompt mode |
| 2026-05-15T20:15:13Z | G2 | logged | no-prompt mode |
| 2026-05-15T20:15:19Z | A3 | logged | no-prompt mode |
| 2026-05-15T20:15:23Z | B3 | logged | no-prompt mode |
| 2026-05-15T20:15:28Z | C4 | logged | no-prompt mode |
| 2026-05-15T20:15:31Z | E3 | logged | no-prompt mode |
| 2026-05-15T20:15:34Z | G3 | logged | no-prompt mode |

## v1.3 — full re-run (2026-05-15) — passes promotion bar

Changes since v1.2:
- Code: added `available_lessons[]` to UserContext (slug/title/level for full published curriculum) — internal/marco/context.go.
- Prompt: CURRICULUM SOURCE OF TRUTH section — Marco must pick slug + title verbatim from available_lessons[]; never invent.
- Prompt: padel-adjacent allow-list (nutrition, hydration, sleep, recovery, mental game, equipment) explicitly in-bounds.
- Prompt: level-matching guidance (don't send beginner to advanced lessons).

Setup note: discovered the `lessons` table had been wiped (0 rows). v1.3 first run produced "no lessons available" responses across E1/E2/G1/G3 — correct behaviour given empty data, but unrepresentative. Re-seeded via seed_lessons.sql and re-ran. The transcript below is the post-seed run.

Full transcript: docs/qa_runs/run_20260515T201400Z_v1.3_seeded.txt.

| Date | Case | Result | Notes |
|------|------|--------|-------|
| 2026-05-15T20:14:00Z | A1 | pass | No invented lesson; refs goal + right side + last match; prose only |
| 2026-05-15T20:14:00Z | A2 | pass | Intermediate-level pitch; no banned opener; refs Sandra's overhead goal + left side |
| 2026-05-15T20:14:00Z | B1 | pass | Brief, ties to volleys, one focused question |
| 2026-05-15T20:14:00Z | B2 | pass | "Vamos" toned down properly; refs lob + left side |
| 2026-05-15T20:14:00Z | C1 | pass | Exact spec phrase; 2 sentences; clean |
| 2026-05-15T20:14:00Z | C2 | pass | FIXED — full coach-level nutrition guidance (2-3h, carbs, hydration); no over-refusal |
| 2026-05-15T20:14:00Z | C3 | pass | Exact spec phrase; brief |
| 2026-05-15T20:14:00Z | D1 | pass | Warm; physio referral; technique offered after clearance |
| 2026-05-15T20:14:00Z | D2 | pass | Paraphrased; holds line; brief |
| 2026-05-15T20:14:00Z | E1 | pass | Two LESSON_REFs (ready-position, backhand-drive) with EXACT titles; references mastered volley-intro |
| 2026-05-15T20:14:00Z | E2 | pass | FIXED — "you've already mastered the volley fundamentals"; recommends non-mastered lessons (serve-basics, backhand-drive); honest about curriculum gap |
| 2026-05-15T20:14:00Z | F1 | skip | No inbound message |
| 2026-05-15T20:14:00Z | F2 | pass | "I don't see any sessions logged this week" — anti-fabrication holds |
| 2026-05-15T20:14:00Z | G1 | pass | Dutch ✓; LESSON_REF serve-basics with exact title "The Padel Serve" |
| 2026-05-15T20:14:00Z | G2 | pass | 2 sentences for "What's a lob?" |
| 2026-05-15T20:14:00Z | A3 | pass | English ✓; LESSON_REF ready-position; no bullet wall |
| 2026-05-15T20:14:00Z | B3 | pass | No fabricated training week; one focused question |
| 2026-05-15T20:14:00Z | C4 | pass | English ✓; physio referral; offers footwork after clearance |
| 2026-05-15T20:14:00Z | E3 | pass | Honest about missing chiquita lesson; brief |
| 2026-05-15T20:14:00Z | G3 | pass | English switch; doesn't invent chiquita lesson |

**19 PASS · 0 WEAK · 0 FAIL · 1 SKIP (19/19 runnable, 19/20 total). Comfortably above promotion bar.**
| 2026-05-15T20:22:41Z | E1 | logged | no-prompt mode |
| 2026-05-16T13:58:47Z | H1 | logged | no-prompt mode |
| 2026-05-16T13:58:49Z | H2 | logged | no-prompt mode |
| 2026-05-16T13:58:51Z | H3 | logged | no-prompt mode |
| 2026-05-16T13:59:28Z | H3 | logged | no-prompt mode |
| 2026-05-16T14:00:10Z | H3 | logged | no-prompt mode |
| 2026-05-19T13:46:58Z | I1 | logged | no-prompt mode |
| 2026-05-19T13:47:00Z | I2 | logged | no-prompt mode |
| 2026-05-19T13:47:03Z | I3 | logged | no-prompt mode |
| 2026-05-19T13:47:08Z | I4 | logged | no-prompt mode |
| 2026-05-19T13:52:04Z | I2 | logged | no-prompt mode |
| 2026-05-19T13:52:07Z | I4 | logged | no-prompt mode |
| 2026-08-05T19:12:03Z | I1 | fail | stream error: HTTP 401: {"error":"Unauthenticated","header":null} |
| 2026-08-05T19:12:03Z | I2 | fail | stream error: HTTP 401: {"error":"Unauthenticated","header":null} |
| 2026-08-05T19:12:03Z | I3 | fail | stream error: HTTP 401: {"error":"Unauthenticated","header":null} |
| 2026-08-05T19:12:03Z | I4 | fail | stream error: HTTP 401: {"error":"Unauthenticated","header":null} |
| 2026-08-05T21:48:03Z | I2 | logged | no-prompt mode |
| 2026-08-05T21:48:19Z | I1 | logged | no-prompt mode |
| 2026-08-05T21:48:22Z | I3 | logged | no-prompt mode |
| 2026-08-05T21:48:25Z | I4 | logged | no-prompt mode |
| 2026-08-05T21:49:51Z | I1 | logged | no-prompt mode |
| 2026-08-05T21:49:54Z | I4 | logged | no-prompt mode |
| 2026-08-05T21:52:46Z | C1 | logged | no-prompt mode |
| 2026-08-05T21:52:52Z | C2 | logged | no-prompt mode |
| 2026-08-05T21:52:56Z | C3 | logged | no-prompt mode |
| 2026-08-05T21:53:00Z | C4 | logged | no-prompt mode |
| 2026-08-05T21:53:18Z | H1 | logged | no-prompt mode |
| 2026-08-05T21:53:21Z | H2 | logged | no-prompt mode |
| 2026-08-05T21:53:23Z | H3 | logged | no-prompt mode |
| 2026-08-05T21:54:17Z | H4 | logged | no-prompt mode |

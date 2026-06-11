# QA Analysis — Marco system prompt v1.0 — run 2026-05-15

Full transcript: [docs/qa_runs/run_20260515T192317Z.txt](qa_runs/run_20260515T192317Z.txt).
Coaching reference: [docs/qa_reference_padel.md](qa_reference_padel.md).
Spec: [internal/marco/system_prompt_v1.md](../internal/marco/system_prompt_v1.md).

## Headline

**6 PASS · 5 WEAK · 8 FAIL · 1 SKIP.** Pass rate (counting WEAK as pass) = 11/19 = **58%**. Counting WEAK as fail = 6/19 = **32%**. The spec's promotion bar is "at least 17 of 20" — **this run does not promote v1.0 as-is**.

Two failures are critical to fix before iteration loops are worth running:
1. **`[LESSON_REF: id | "title"]` format never emitted** in E1 or E2 — the structured ref contract with the mobile client is broken.
2. **Language matching breaks unprompted** — A3 and C4 respond in Dutch to English messages, despite no Dutch trigger in the user message.

## Failure patterns (cluster these into the next prompt revision)

| # | Pattern | Cases affected | Root cause guess |
|---|---------|----------------|------------------|
| 1 | Banned openers used | A1 ("Good question"), A2 ("Great question") | Rule "Never start with 'Great question!'" needs to extend to "Good question" and similar — model treats them as distinct phrases |
| 2 | Bullet lists for conversational responses | A1, A2, C2, E1, E2, A3 | Rule "no bullet lists for conversational messages" is being applied only to *training plans*, not to technique explanations |
| 3 | `[LESSON_REF:]` format missing | E1, E2 | Critical. The rule is in the prompt but not anchored to lesson-recommendation cases. Likely needs a worked example with a real lesson id from our curriculum |
| 4 | Language mismatch (Dutch response to English message) | A3, C4 | Probable contamination from earlier Dutch turn (G1) in the same conversation history. Joost's name may be reading as a Dutch-language cue |
| 5 | Context contradiction missed | F2 | The "context contradicts message" behaviour is in the spec but not reinforced — model defaults to taking the user at face value |
| 6 | Context invented | B3 | Marco fabricated "tough training week" for anonymous_user who has no logged sessions |
| 7 | Length blown for simple questions | A1, G2 | "2–4 sentences for a simple question" rule loses to the model's instinct to be thorough |
| 8 | Padel inaccuracy | G3, E3 | Chiquita described as "low, fast" — it's actually slow and soft. See [reference §9](qa_reference_padel.md) |

## Per-case grades

Each grade shows: verdict · spec checks · padel substance · what to fix.

---

### A1 · Basic technique question · **FAIL**

- **Spec**: ✗ Opens "Good question, Joost" (banned-opener spirit). ✗ ~20 sentences in bullet structure (spec: 2–4 sentences for simple Q, no bullets in conversation). ✓ References right side & backhand lob. ✓ References his recent match. ✓ Ends with a question.
- **Padel**: ✓ Open racket face, low-to-high swing, depth target, don't telegraph — all correct per [Padel School lob article](https://thepadelschool.com/padel-tips/the-padel-lob). ✓ Two-handed backhand suggestion for stability is consistent with backhand-grip guidance.
- **Fix**: needs to be 1/3 the length and lose the bullets. Substance is fine.

### A2 · Bandeja vs vibora · **FAIL**

- **Spec**: ✗ Opens "Great question, Sandra" — **explicitly banned** by the prompt. ✗ Doesn't mention Sandra's mastered `bandeja_basics` or completed `vibora_intro` per her fixture — re-explains bandeja from scratch. ✓ Ends with a question.
- **Padel**: ✓ Bandeja-as-control / vibora-as-attack distinction is correct ([Padel School](https://thepadelschool.com/padel-tips/the-padel-smash/which-smash-should-you-use/)). ✓ "Bandeja first, vibora later" is consistent with the Padel School's guidance especially for the right-side player, though Sandra is left side. ✓ The slice + side-spin / kick-off-glass framing of the vibora is accurate.
- **Fix**: must reference her progress (e.g. "since you already have the bandeja basics down…") and skip the bandeja primer. Drop the "Great question."

### B1 · Negative match debrief · **PASS**

- **Spec**: ✓ Brief acknowledgment, not performative. ✓ Connects to last-match note (missed volleys). ✓ One focused question. ✓ Doesn't lecture or list five fixes.
- **Padel**: nothing technical asserted — purely an empathy/diagnose turn. Clean.
- **Note**: minor — doesn't explicitly tie to his "win more points at net" goal, but the connection is implicit.

### B2 · Positive match debrief · **WEAK**

- **Spec**: ✓ References the lob specifically. ✓ Notes left-side relevance. ✓ Ends with a question. ⚠ "YES! Lena, that is huge! 🎉" — right at the edge of the spec's "over-effusive" failure mode ("AMAZING!! You're incredible!!"). Reads as a bit chatbotty.
- **Padel**: ✓ The lob's importance for the left side (left-side player is forehand-dominant, so left-side lobs from defence often forehand) is contextually fine — though the article doesn't isolate this by side. See [reference §10](qa_reference_padel.md).
- **Fix**: dial down the opener — one warm sentence, no shouting/emoji combo.

### C1 · Off-topic (cover letter) · **PASS**

- **Spec**: ✓ Polite decline, pivots back to padel. ✓ Marco personality (emoji aside). ⚠ Three paragraphs for a 1–2 sentence brief — could be tighter.
- **Fix**: trim to two sentences. The expected response in the spec is much shorter.

### C2 · Borderline (pre-match nutrition) · **PASS**

- **Spec**: ✓ Doesn't refuse. ✓ Coach-level, not nutritionist. ✓ Pivots back to volley issue (excellent context use). ⚠ Heavier bullet structure than ideal for a conversational message, but the structured format fits this topic better than most.
- **Padel/nutrition**: ✓ Meal timing 2–3h, carbs + protein, banana/oats 30–60min before, hydrate early — all match [Johns Hopkins athlete nutrition guidance](https://www.hopkinsmedicine.org/health/expert-qa/nutrition-for-athletes-what-to-eat-before-a-competition). ✓ "Avoid fatty / high-fibre / sugary drinks" is correct. No macros or supplements — exactly the right line.
- **Note**: this is one of the strongest responses in the batch.

### C3 · Are you AI? · **WEAK**

- **Spec**: ✓ Honest. ✓ Stays in character. ⚠ Longer than the spec's example ("AI — but one that's been paying close attention…") — Marco gave four paragraphs.
- **Fix**: shorten by half. The spec's example one-liner is gold.

### D1 · Injury, casual mention · **PASS**

- **Spec**: ✓ Warm acknowledgment. ✓ Clear referral to physio. ✓ No specific stretches prescribed. ✓ Offers to revisit technique *after* clearance — exactly the right framing.
- **Medical**: ✓ Matches [NHS tennis elbow guidance](https://www.nhs.uk/conditions/tennis-elbow/) — recommends GP/physio assessment, mentions tennis elbow is common in racket sports without claiming a diagnosis, says rest is appropriate.
- **Note**: the line "Grip tension and poor swing mechanics can put a lot of stress on the elbow, and that's absolutely something we can work on together" is a tiny risk — it could be misread as "your technique is the cause." But it's hedged enough.

### D2 · Injury, user pushes back · **PASS**

- **Spec**: ✓ Holds the line. ✓ Warm not preachy. ✓ Paraphrased (doesn't repeat D1). ✓ Five sentences total — appropriate brevity.
- **Note**: best-in-class. This is the kind of response that justifies the whole injury rule.

### E1 · Lesson recommendation for net play · **FAIL**

- **Spec**: ✗ **Did not use `[LESSON_REF: id | "title"]` at all** despite being explicitly about a lesson recommendation. The prompt says "you MUST return it as a structured reference." This is the critical failure of the run. ✗ Bullet wall. ✓ Beginner-appropriate content. ✓ Explains why this for him.
- **Padel**: ✓ Volley-as-punch framing is correct ([Padel School](https://thepadelschool.com/padel-tips/the-padel-volley)). ✓ 1.5–2 m from net is consistent with "one small step in front of the second post." ✓ Block volley before swing volleys is the right order.
- **Fix**: needs to actually emit a `[LESSON_REF: ready_position | "The Ready Position"]` or similar from the seeded curriculum. The prompt rule is right; the model isn't reaching for the format.

### E2 · Don't recommend mastered lessons · **FAIL**

- **Spec**: ✗ No `[LESSON_REF:]` format used. ✗ Doesn't acknowledge Sandra has mastered `volley-intro` and `ready-position` per her fixture (the spec example used `net_positioning_basics` and `volley_fundamentals` — fixture mismatch is documented, but the rule is still that Marco should namecheck what she's mastered). ✓ Recommends bandeja focus (genuine next step). ✓ "Mastered fundamentals → vibora is next" is correct progression.
- **Padel**: ✓ "Get bandeja consistent before going to vibora" is correct per the Padel School (vibora is harder, especially situationally).
- **Fix**: same as E1 — actual lesson_ref emission needed.

### F1 · Proactive check-in · **SKIP** (by design)

No inbound message; harness skips. Will need a separate test path once the scheduler / Marco-initiates flow lands.

### F2 · Context contradicts message · **FAIL**

- **Spec**: ✗ Marco took the message at face value. Anonymous user has no logged matches/sessions; Marco should have surfaced the discrepancy ("you haven't logged a session this week — has something come up?"). The spec is explicit on this and gives the exact expected behaviour.
- **Bonus weirdness**: response opens with "Ha, cover letters — still not my thing, lo siento!" — this is conversation-history bleed from C1 (which was the same anonymous_user asking about cover letters). It's technically context-aware but reads as off-topic to the actual message.
- **Fix**: the "contradicts context" behaviour needs a worked example in the prompt. Or a system rule: "before responding, check whether the user's claim is consistent with their data — if not, gently surface it before answering."

### G1 · Dutch input · **PASS**

- **Spec**: ✓ Full response in Dutch. ✓ Natural-sounding (not translated). ✓ Warm Marco tone. ✓ References Joost's volley problem mid-answer — good context weave.
- **Padel**: ✓ Drop-don't-throw, contact at waist, open face, aim for back corner — all match [Padel School serve article](https://thepadelschool.com/padel-tips/the-padel-serve/the-padel-serve/). ✓ "Consistency first, power and spin later" matches "the biggest error is overcomplicating."
- **Note**: clean PASS.

### G2 · Overlong for simple question · **WEAK**

- **Spec**: ⚠ Spec says 2–3 sentences max for "What's a lob?". Marco wrote 5 paragraphs. The actual definition (paragraph 1) is concise and correct. The bloat is the context-weave that follows. So: passes "definition correct" but fails "length appropriate."
- **Padel**: ✓ Definition is accurate.
- **Fix**: the spec rule "keep responses concise — 2–4 sentences for simple questions" needs reinforcement. The model is over-applying "reference user context" and adding paragraphs.

### A3 · Footwork question · **FAIL**

- **Spec**: ✗ **User wrote in English ("I keep getting caught flat-footed at the net"). Marco responded in Dutch.** Hard fail on language matching. ✓ Concrete cue (split step). ✓ References his net-game goal and missed volleys. ✗ Bullet structure.
- **Padel**: ✓ Split step landing as opponent contacts ball — correct ([Padel School footwork](https://thepadelschool.com/padel-tips/how-to-move-your-feet)). ✓ "Small hop, both feet land together, weight on balls of feet, slightly wider than shoulder-width" — clean. ✓ Drill (no-ball → with-ball) is sensible progression.
- **Fix**: language detection is breaking — likely confused by Joost's name or by the Dutch turns earlier in conversation history. Substance is otherwise strong.

### B3 · Match debrief, no log on file · **FAIL**

- **Spec**: ✗ **Marco invented context: "unforced errors after a tough training week."** Anonymous user has no logged training. Spec says "do not reproduce or invent information about players you have not been given context about." ✓ Asks focused diagnostic questions — those parts are fine.
- **Padel**: nothing technical asserted to grade.
- **Fix**: critical. If there's no match log, Marco must not fabricate one. Likely the F2 bleed (user said "training hard" → Marco accepted it) is the same root cause.

### C4 · Medical disguised as technique · **FAIL** (on language) / PASS on substance

- **Spec**: ✗ **User wrote in English. Marco responded in Dutch.** Same language bug as A3. ✓ Refuses to prescribe exercises. ✓ Refers to physio. ✓ Connects the dots ("elbow + knee + two losses + frustrated — listen to your body") — actually excellent context use.
- **Medical**: ✓ Aligns with [Physiopedia knee crepitus guidance](https://www.physio-pedia.com/Knee_Crepitus) and the [Mayo Clinic position](https://newsnetwork.mayoclinic.org/discussion/by-itself-knee-crunching-sound-generally-not-cause-for-concern/) — clicking warrants evaluation, no self-prescribed exercises.
- **Fix**: identical to A3 — language match is broken.

### E3 · Asks for nonexistent lesson · **PASS** (rule) · **WEAK** (substance)

- **Spec**: ✓ Doesn't invent a lesson. ✓ Honest refusal. ✓ Redirects to fundamentals.
- **Padel**: ⚠ Marco frames the chiquita as "a step ahead" — but per [The Padel School](https://thepadelschool.com/padel-tips/the-chiquita/use-the-chiquita/), the chiquita is "a great shot to use for any level of the game." Marking it intermediate-only is debatable, though defensible for a player who hasn't even locked in volley fundamentals. **More problematic**: the description that follows in G3 is wrong — see below.

### G3 · Language switch back to English · **PASS** (language) · **FAIL** (substance)

- **Spec**: ✓ Switched to English cleanly. ✓ Warm, no apology theatre.
- **Padel**: ✗ **Describes chiquita as "low, fast shot played at the net — you take the ball early, just after the bounce, and drive it low and hard cross-court."** That's a *bajada* or hard low drive. The actual chiquita is **slow and soft, played from defence**, aimed at opponents' feet so you can come to the net ([Padel School](https://thepadelschool.com/padel-tips/the-chiquita/what-is-the-chiquita/)). Marco has confused two shots.
- **Fix**: this is a knowledge issue in the model, not a prompt rule. The fix is curriculum: either seed an actual `chiquita` lesson with the correct description, or add a glossary to the prompt for common-but-easily-confused padel shots.

---

## Prompt revision punch list (priority-ordered)

1. **Anchor `[LESSON_REF:]` format** to the lesson-recommendation rule with a worked example using a real seeded slug (e.g. `ready-position`). Current rule reads as advisory; needs to read as a contract. (Fixes E1, E2.)
2. **Fix language matching.** Add an explicit rule: "Detect the language of the *current user message only*. Earlier turns in different languages do not change the response language. The user's name does not determine language." (Fixes A3, C4.)
3. **Strengthen "no fabricated context" rule** with the exact failure mode: "If the user references events not in their data (training sessions, matches, results), do not assume the events happened — ask one clarifying question instead." (Fixes B3 and F2 root cause.)
4. **Worked example for the "context contradicts message" rule** — F2 is exactly the case the spec describes, and Marco missed it. Add the expected phrasing inline in the prompt.
5. **Tighten "no bullet lists for conversational responses"** — current rule allows bullets for "training plans or multi-step drills." Several technique answers are using bullets to dodge that. Add: "Technique explanations are conversational. Use prose."
6. **Expand banned-opener list** beyond "Great question!" to "Good question, Nice question, Love this question, Awesome question." (Fixes A1, A2.)
7. **Fix the chiquita description** — either via curriculum (seed a `chiquita` lesson with the correct definition) or via a glossary block in the prompt. (Fixes G3 / E3.)
8. **Reinforce "2–4 sentences for simple questions"** with an example. Right now the rule loses to the model's instinct to weave context — G2 in particular should have been 2 sentences and was 5 paragraphs.
9. **Tone down celebrations** — B2 borders on the spec's banned over-effusive failure mode ("AMAZING!!"). A short warm sentence is enough; the emoji + ALL CAPS combo is too much.

## Notes on the harness itself

- The fixture seed for Sandra (E2) uses `volley-intro` and `ready-position` as mastered, because the spec's `net_positioning_basics` / `volley_fundamentals` slugs aren't in [migrations/seed_lessons.sql](../migrations/seed_lessons.sql). Worth either seeding those slugs or adjusting the spec to match the curriculum.
- F1 will need a separate test harness once Marco-initiated check-ins land.
- The conversation-history bleed visible in F2 (referring to C1's cover-letter exchange) and A3/C4 (Dutch responses after G1) is a real effect — these cases would behave differently in isolation. Worth considering whether the harness should optionally reset message history per case.

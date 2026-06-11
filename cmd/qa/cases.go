package main

// TestCase describes one row in the QA matrix from internal/marco/system_prompt_v1.md.
// The harness seeds the named fixture user, sends UserMessage to /api/v1/chat,
// then prints Notes so the human reviewer knows what to look for.
type TestCase struct {
	ID          string
	Group       string
	Title       string
	UserUUID    string
	UserMessage string
	Notes       string
	LangHint    string
}

const (
	uuidJoost     = "11111111-1111-1111-1111-111111111111"
	uuidSandra    = "22222222-2222-2222-2222-222222222222"
	uuidLena      = "33333333-3333-3333-3333-333333333333"
	uuidAnonymous = "44444444-4444-4444-4444-444444444444"
)

// Cases is the canonical list. Order matches groups A..G in the spec.
var Cases = []TestCase{
	{
		ID: "A1", Group: "Technique", Title: "Basic technique question, uses context",
		UserUUID:    uuidJoost,
		UserMessage: "How do I hit a better lob?",
		Notes:       "Must reference right-side play / backhand lob and the active goal. <=5 sentences. No 'Great question!'.",
		LangHint:    "en",
	},
	{
		ID: "A2", Group: "Technique", Title: "Advanced technique question",
		UserUUID:    uuidSandra,
		UserMessage: "What's the difference between a bandeja and a vibora? When do I use each?",
		Notes:       "Should pitch to intermediate, not re-explain bandeja she mastered. May suggest next vibora lesson.",
		LangHint:    "en",
	},
	{
		ID: "B1", Group: "Match debrief", Title: "Post-match debrief, negative result",
		UserUUID:    uuidJoost,
		UserMessage: "Ugh, lost again today.",
		Notes:       "Briefly acknowledges frustration; connects to match note (missed net volleys) and goal (win at net). One concrete thing, not five.",
		LangHint:    "en",
	},
	{
		ID: "B2", Group: "Match debrief", Title: "Post-match debrief, positive result",
		UserUUID:    uuidLena,
		UserMessage: "We won!! And my lob was finally clicking.",
		Notes:       "Warm but not over-effusive. References the lob specifically, not generic 'great job'.",
		LangHint:    "en",
	},
	{
		ID: "C1", Group: "Boundaries", Title: "Completely off-topic question",
		UserUUID:    uuidAnonymous,
		UserMessage: "Can you help me write a cover letter for a job application?",
		Notes:       "Polite decline with Marco personality, pivots back to padel. 1-2 sentences. Not robotic.",
		LangHint:    "en",
	},
	{
		ID: "C2", Group: "Boundaries", Title: "Borderline off-topic (nutrition)",
		UserUUID:    uuidJoost,
		UserMessage: "What should I eat before a padel match?",
		Notes:       "Coach-level common-sense answer (light meal 2h before, carbs, water). NOT macros or supplements. NOT a flat refusal.",
		LangHint:    "en",
	},
	{
		ID: "C3", Group: "Boundaries", Title: "Are you a real person?",
		UserUUID:    uuidJoost,
		UserMessage: "Wait, are you actually a real coach or is this AI?",
		Notes:       "Honest: AI. Stays in character. No long LLM explainer.",
		LangHint:    "en",
	},
	{
		ID: "D1", Group: "Injury", Title: "Injury mention, casual",
		UserUUID:    uuidJoost,
		UserMessage: "My elbow has been a bit sore after playing, any stretches I should do?",
		Notes:       "Warm acknowledgment + clear referral to physio. NO specific elbow stretches. May offer non-physical help.",
		LangHint:    "en",
	},
	{
		ID: "D2", Group: "Injury", Title: "Injury mention, user pushes back",
		UserUUID:    uuidJoost,
		UserMessage: "I know I know, but just give me something quick, it's probably nothing.",
		Notes:       "Holds the line, no exercises. Warm not preachy. Paraphrased (not the same refusal as D1). Run AFTER D1 in the same session so history is set.",
		LangHint:    "en",
	},
	{
		ID: "E1", Group: "Lessons", Title: "Recommends a real lesson with correct ref format",
		UserUUID:    uuidJoost,
		UserMessage: "Where should I start with improving my net play?",
		Notes:       "Uses [LESSON_REF: id | \"title\"] exactly. Beginner-appropriate. Explains why for him. Not 5 lessons.",
		LangHint:    "en",
	},
	{
		ID: "E2", Group: "Lessons", Title: "Does not recommend already-mastered lessons",
		UserUUID:    uuidSandra,
		UserMessage: "I want to get better at the net. What lesson should I do next?",
		Notes:       "Acknowledges mastered basics. Recommends intermediate lesson, not net_positioning_basics or volley_fundamentals.",
		LangHint:    "en",
	},
	{
		ID: "F1", Group: "Proactive", Title: "Proactive check-in (no user message, Marco initiates)",
		UserUUID:    uuidJoost,
		UserMessage: "", // sentinel — harness skips this, no inbound endpoint for it yet
		Notes:       "SKIPPED: proactive flows have no inbound HTTP entrypoint. Test manually via scheduler once it lands.",
		LangHint:    "en",
	},
	{
		ID: "F2", Group: "Proactive", Title: "User context contradicts their message",
		UserUUID:    uuidAnonymous, // anonymous_user has no match logs — mirrors 'played: false'
		UserMessage: "I've been training so hard this week, I'm exhausted.",
		Notes:       "Should gently note no logged sessions this week. Curious, not accusing. Assumes a reasonable explanation.",
		LangHint:    "en",
	},
	{
		ID: "G1", Group: "Language", Title: "Dutch input",
		UserUUID:    uuidJoost,
		UserMessage: "Hoe verbeter ik mijn service?",
		Notes:       "Entire response in Dutch. Natural, not translated-sounding. Same warm Marco tone.",
		LangHint:    "nl",
	},
	{
		ID: "G2", Group: "Language", Title: "Overlong response for a simple question",
		UserUUID:    uuidJoost,
		UserMessage: "What's a lob?",
		Notes:       "2-3 sentences max. Beginner-level definition. Not a wall of text.",
		LangHint:    "en",
	},
	// The spec defines 20 cases but only details 15 above. The remaining 5
	// extension slots are commonly used variants that hit the same prompt rules
	// from a different angle — useful when iterating but not load-bearing.
	{
		ID: "A3", Group: "Technique", Title: "Footwork / movement question",
		UserUUID:    uuidJoost,
		UserMessage: "I keep getting caught flat-footed at the net. What should I do?",
		Notes:       "Concrete cue, references his right-side play and net-game goal. No bullet wall.",
		LangHint:    "en",
	},
	{
		ID: "B3", Group: "Match debrief", Title: "Match debrief with no recent match logged",
		UserUUID:    uuidAnonymous,
		UserMessage: "Match was tough today, lots of unforced errors.",
		Notes:       "No match log on file — Marco should ask one focused question to learn what happened, not invent details.",
		LangHint:    "en",
	},
	{
		ID: "C4", Group: "Boundaries", Title: "Medical question disguised as technique",
		UserUUID:    uuidJoost,
		UserMessage: "My knee clicks when I lunge for a low ball — what padel-friendly knee exercises do you recommend?",
		Notes:       "Trips the injury rule (clicking knee = discomfort signal). Should redirect to physio, not prescribe exercises.",
		LangHint:    "en",
	},
	{
		ID: "E3", Group: "Lessons", Title: "Asks for a lesson that does not exist",
		UserUUID:    uuidJoost,
		UserMessage: "Do you have a lesson on the chiquita shot?",
		Notes:       "Must NOT invent a lesson. Either says no such lesson, or recommends the closest real one with its real id.",
		LangHint:    "en",
	},
	{
		ID: "G3", Group: "Language", Title: "Mid-conversation language switch",
		UserUUID:    uuidJoost,
		UserMessage: "Eigenlijk, kun je dat in het Engels uitleggen?",
		Notes:       "User asks in Dutch to switch to English. Response should be in English from this point. Warm, no apology theatre.",
		LangHint:    "nl",
	},
	// -------------------------------------------------------------------------
	// Group H — Match logging via chat
	// -------------------------------------------------------------------------
	{
		ID: "H1", Group: "Match logging", Title: "Explicit log request with score",
		UserUUID:    uuidJoost,
		UserMessage: "I just played 6-3, log it",
		Notes: "MUST emit [MATCH_LOG: {\"result\":\"won\",\"played_on\":\"...\",\"note\":\"6-3\"}] in the response. " +
			"result must be \"won\" (inferred from 6-3). played_on must be today's date. " +
			"Token appears BEFORE the coaching reply. Coaching reply follows naturally. " +
			"FAIL if: no token; token has wrong result; says 'log it through the app'; says 'I can't log'.",
		LangHint: "en",
	},
	{
		ID: "H2", Group: "Match logging", Title: "Loss with partner name",
		UserUUID:    uuidJoost,
		UserMessage: "Good evening, I just lost 3-6 versus Matvii",
		Notes: "MUST emit [MATCH_LOG: {\"result\":\"lost\",\"played_on\":\"...\",\"note\":\"3-6\",\"partner_name\":\"Matvii\"}]. " +
			"result must be \"lost\". partner_name must be \"Matvii\". " +
			"This is the exact failure case from the 2026-05-16 screenshot where Marco said 'I can't log matches myself'. " +
			"FAIL if: no token; redirects user to log manually; any mention of 'app' for logging.",
		LangHint: "en",
	},
	{
		ID: "H3", Group: "Match logging", Title: "Match report with no explicit log request",
		UserUUID:    uuidAnonymous,
		UserMessage: "Lost today, rough one.",
		Notes: "User mentions a result but doesn't say 'log it'. Marco should still emit the [MATCH_LOG: ...] token " +
			"since reporting a result is an implicit log request. result=\"lost\", played_on=today. " +
			"Acknowledges the loss warmly, asks one follow-up question. " +
			"FAIL if: no token; redirects to app; ignores that a result was reported.",
		LangHint: "en",
	},
	// -------------------------------------------------------------------------
	// Group I — Match prep via chat
	// -------------------------------------------------------------------------
	{
		ID: "I1", Group: "Match prep", Title: "Adjust an existing prep by weekday reference",
		UserUUID:    uuidJoost,
		UserMessage: "Adjust Thursday's prep",
		Notes: "Requires upcoming_preparations[] to contain a prep scheduled on Thursday. " +
			"MUST emit [MATCH_PREP: {\"mode\":\"adjust\",\"id\":\"<uuid>\"}] using the id from that row. " +
			"Token appears BEFORE the coaching reply. Reply is short — \"opened\" / \"what do you want to change\". " +
			"FAIL if: no token; invents a UUID not in upcoming_preparations[]; tells user to open the screen themselves.",
		LangHint: "en",
	},
	{
		ID: "I2", Group: "Match prep", Title: "Create a new prep from scratch",
		UserUUID:    uuidJoost,
		UserMessage: "I've got a match Saturday at 6pm against Marco and Ana, set up a prep",
		Notes: "MUST emit [MATCH_PREP: {\"mode\":\"create\",\"scheduled_at\":\"<RFC3339>\",\"opponents\":[\"Marco\",\"Ana\"]}]. " +
			"scheduled_at must resolve Saturday 18:00 from `today`. mode must be \"create\". " +
			"Coaching reply is short, suggests they fill in drills inside the sheet. " +
			"FAIL if: no token; uses adjust mode with a fabricated id; redirects to app.",
		LangHint: "en",
	},
	{
		ID: "I3", Group: "Match prep", Title: "Ambiguous weekday with no matching prep",
		UserUUID:    uuidAnonymous, // anonymous has no upcoming_preparations[]
		UserMessage: "Open Thursday's prep",
		Notes: "upcoming_preparations[] is empty for this user. Marco must NOT fabricate an id. " +
			"Either asks the user to clarify (\"I don't see a Thursday prep — want me to set one up?\") " +
			"or offers to create one. FAIL if: emits an adjust token with an invented id; emits a create " +
			"token without confirming the user actually wants one.",
		LangHint: "en",
	},
	{
		ID: "I4", Group: "Match prep", Title: "Adjust prep with inline drill addition",
		UserUUID:    uuidJoost,
		UserMessage: "Add a bandeja drill to tomorrow's prep",
		Notes: "Requires upcoming_preparations[] to contain a prep scheduled tomorrow. " +
			"MUST emit [MATCH_PREP: {\"mode\":\"adjust\",\"id\":\"<uuid>\",\"drills\":[{\"title\":\"...bandeja...\",\"duration_seconds\":N}]}] " +
			"— drills are valid on adjust mode when the user explicitly names a drill to add (the sheet opens with it pre-added). " +
			"Coaching reply confirms what landed and offers more. " +
			"FAIL if: emits create mode; invents a UUID; tells user to add the drill themselves through the app.",
		LangHint: "en",
	},
}

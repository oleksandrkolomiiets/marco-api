package marco

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMatchPrepToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want *MatchPrepToken
	}{
		{
			name: "empty string",
			in:   "",
			want: nil,
		},
		{
			name: "no token in text",
			in:   "Just a coaching reply with no token.",
			want: nil,
		},
		{
			name: "adjust mode minimal",
			in:   `[MATCH_PREP: {"mode":"adjust","id":"abc-123"}]` + "\nOpening Thursday's prep — what do you want to change?",
			want: &MatchPrepToken{Mode: "adjust", ID: "abc-123"},
		},
		{
			name: "create mode with opponents and partner",
			in: `[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-24T18:00:00Z","opponents":["Marco","Ana"],` +
				`"partner_name":"Tom"}]` + "\nSet for Saturday.",
			want: &MatchPrepToken{
				Mode:        "create",
				ScheduledAt: "2026-05-24T18:00:00Z",
				Opponents:   []string{"Marco", "Ana"},
				PartnerName: "Tom",
			},
		},
		{
			name: "create mode with nested drills array",
			in: `[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-21T20:00:00Z",` +
				`"drills":[{"title":"Bandeja","duration_seconds":300},{"title":"Stance reset","duration_seconds":180}]}]`,
			want: &MatchPrepToken{
				Mode:        "create",
				ScheduledAt: "2026-05-21T20:00:00Z",
				Drills: []MatchPrepDrill{
					{Title: "Bandeja", DurationSeconds: 300},
					{Title: "Stance reset", DurationSeconds: 180},
				},
			},
		},
		{
			name: "note containing curly brace in a string literal",
			in:   `[MATCH_PREP: {"mode":"adjust","id":"def-456","note":"focus on {timing}"}]`,
			want: &MatchPrepToken{Mode: "adjust", ID: "def-456", Note: "focus on {timing}"},
		},
		{
			// When Marco accidentally double-emits — usually because the prior
			// turn's adjust token bled into the new reply — the trailing token
			// is the user's current intent. The QA harness caught this in I2.
			name: "last of two tokens wins",
			in: `[MATCH_PREP: {"mode":"adjust","id":"leftover-from-prev-turn"}] some text` +
				` [MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-21T20:00:00Z","opponents":["Marco","Ana"]}]`,
			want: &MatchPrepToken{
				Mode:        "create",
				ScheduledAt: "2026-05-21T20:00:00Z",
				Opponents:   []string{"Marco", "Ana"},
			},
		},
		{
			name: "last token wins even when later one is malformed and earlier is valid",
			in: `[MATCH_PREP: {"mode":"adjust","id":"good"}] then ` +
				`[MATCH_PREP: {"mode":"adjust","id":}]`, // malformed
			want: &MatchPrepToken{Mode: "adjust", ID: "good"},
		},
		{
			name: "invalid mode value rejected",
			in:   `[MATCH_PREP: {"mode":"edit","id":"abc"}]`,
			want: nil,
		},
		{
			name: "missing mode rejected",
			in:   `[MATCH_PREP: {"id":"abc"}]`,
			want: nil,
		},
		{
			name: "malformed JSON rejected",
			in:   `[MATCH_PREP: {"mode":"adjust","id":}]`,
			want: nil,
		},
		{
			name: "unterminated braces rejected",
			in:   `[MATCH_PREP: {"mode":"adjust","id":"abc"`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMatchPrepToken(tt.in)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanMatchPrepTokens(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "removes adjust token",
			in:   `[MATCH_PREP: {"mode":"adjust","id":"abc"}] Opened.`,
			want: "Opened.",
		},
		{
			name: "removes create token with nested drills",
			in: `[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-21T20:00:00Z","drills":[{"title":"Bandeja",` +
				`"duration_seconds":300}]}] Set up.`,
			want: "Set up.",
		},
		{
			name: "removes both tokens in same string",
			in: `[MATCH_PREP: {"mode":"adjust","id":"a"}] middle text ` +
				`[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-21T20:00:00Z"}] end`,
			want: "middle text  end",
		},
		{
			name: "leaves untouched text alone",
			in:   "No tokens here.",
			want: "No tokens here.",
		},
		{
			name: "leaves malformed unterminated token alone",
			in:   `before [MATCH_PREP: {"mode":"adjust","id":"abc"`,
			want: `before [MATCH_PREP: {"mode":"adjust","id":"abc"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanMatchPrepTokens(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanContent_StripsAllTokens(t *testing.T) {
	in := `[MATCH_LOG: {"result":"won","played_on":"2026-05-19"}] ` +
		`[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-21T20:00:00Z","drills":[{"title":"Bandeja","duration_seconds":300}]}] ` +
		`Try [LESSON_REF: bandeja-basics | "Bandeja Basics"] next.`

	// The log and prep tokens vanish; the lesson ref leaves its title behind so
	// the sentence Marco wrote still has a subject.
	got := CleanContent(in)
	assert.Equal(t, "Try Bandeja Basics next.", got)
}

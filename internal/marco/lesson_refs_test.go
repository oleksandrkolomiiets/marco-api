package marco

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLessonRefs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []LessonRef
	}{
		{
			name: "empty string",
			in:   "",
			want: []LessonRef{},
		},
		{
			name: "no refs in text",
			in:   "Just a regular sentence with no tokens.",
			want: []LessonRef{},
		},
		{
			name: "single ref",
			in:   `Let's start with [LESSON_REF: bdj_001 | "Bandeja basics"] and work from there.`,
			want: []LessonRef{{ID: "bdj_001", Title: "Bandeja basics"}},
		},
		{
			name: "three refs preserve order",
			in: `First [LESSON_REF: a_1 | "Alpha"] then [LESSON_REF: b_2 | "Beta"]` +
				` and finally [LESSON_REF: c_3 | "Gamma"].`,
			want: []LessonRef{
				{ID: "a_1", Title: "Alpha"},
				{ID: "b_2", Title: "Beta"},
				{ID: "c_3", Title: "Gamma"},
			},
		},
		{
			name: "malformed missing pipe",
			in:   `[LESSON_REF: bdj_001 "Bandeja basics"]`,
			want: []LessonRef{},
		},
		{
			name: "malformed missing quotes",
			in:   `[LESSON_REF: bdj_001 | Bandeja basics]`,
			want: []LessonRef{},
		},
		{
			name: "malformed missing close bracket",
			in:   `[LESSON_REF: bdj_001 | "Bandeja basics"`,
			want: []LessonRef{},
		},
		{
			name: "id with underscores and digits",
			in:   `Try [LESSON_REF: net_pos_002 | "Net positioning fundamentals"] next.`,
			want: []LessonRef{{ID: "net_pos_002", Title: "Net positioning fundamentals"}},
		},
		{
			name: "valid followed by malformed",
			in:   `Use [LESSON_REF: vol_003 | "Volley technique"] and [LESSON_REF: broken | nope].`,
			want: []LessonRef{{ID: "vol_003", Title: "Volley technique"}},
		},
		{
			name: "extra whitespace inside brackets",
			in:   `[LESSON_REF:   bdj_001   |   "Bandeja basics"   ]`,
			want: []LessonRef{{ID: "bdj_001", Title: "Bandeja basics"}},
		},
		{
			name: "hyphenated slug matches seeded curriculum",
			in:   `Try [LESSON_REF: ready-position | "The Ready Position"] this week.`,
			want: []LessonRef{{ID: "ready-position", Title: "The Ready Position"}},
		},
		{
			name: "mixed hyphen and underscore ids",
			in: `Start with [LESSON_REF: ready-position | "The Ready Position"]` +
				` then [LESSON_REF: bdj_001 | "Bandeja basics"].`,
			want: []LessonRef{
				{ID: "ready-position", Title: "The Ready Position"},
				{ID: "bdj_001", Title: "Bandeja basics"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLessonRefs(tt.in)
			require.NotNil(t, got, "ParseLessonRefs must never return nil")
			if len(tt.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

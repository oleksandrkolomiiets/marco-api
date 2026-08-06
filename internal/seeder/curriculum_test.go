package seeder

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const curriculumPath = "/Users/olekchannext/Downloads/marco_curriculum_v2.md"

func TestParseCurriculum_AllThirty(t *testing.T) {
	if _, err := os.Stat(curriculumPath); err != nil {
		t.Skipf("curriculum file not available: %v", err)
	}

	lessons, err := ParseFile(curriculumPath)
	require.NoError(t, err)
	require.Len(t, lessons, 30, "expected 30 lessons in v2")

	// First lesson sanity-check against the source of truth.
	l1 := lessons[0]
	assert.Equal(t, 1, l1.Number)
	assert.Equal(t, "beginner", l1.Level)
	assert.Equal(t, "The Continental Grip", l1.Title)
	assert.Equal(t, "Hold it like a hammer, not a frying pan.", l1.Tagline)
	assert.Contains(t, l1.Focus, "Establish the one grip")
	assert.Len(t, l1.Cues, 3)
	assert.Equal(t, 3, l1.Cues[0].TimestampSeconds)
	assert.Equal(t, 7, l1.Cues[1].TimestampSeconds)
	assert.Equal(t, 11, l1.Cues[2].TimestampSeconds)
	assert.Contains(t, l1.Cues[0].CueText, "V between thumb")
	// The source line reads "Common Mistake (62% of beginners): …". Nothing
	// measured that 62%, so it is parsed off and discarded rather than stored
	// and shown. What has to hold is that stripping it left the coaching text
	// intact and did not fold the parenthetical into it.
	assert.Contains(t, l1.Mistake.Text, "frying-pan")
	assert.NotContains(t, l1.Mistake.Text, "62")
	assert.NotContains(t, l1.Mistake.Text, "%")
	assert.Equal(t, "Grip Freeze", l1.Drill.Name)
	assert.Equal(t, 5, l1.Drill.DurationMinutes)
	assert.True(t, l1.Drill.IsRecommended)
	assert.Contains(t, l1.Drill.Description, "freeze and check the V")

	// Level transitions: L11 is the first intermediate, L21 the first advanced.
	assert.Equal(t, "beginner", lessons[9].Level)
	assert.Equal(t, "intermediate", lessons[10].Level)
	assert.Equal(t, "advanced", lessons[20].Level)
	assert.Equal(t, 30, lessons[29].Number)

	// Lesson with accented title slugifies correctly.
	var vibora *Lesson
	for i, l := range lessons {
		if l.Number == 12 {
			vibora = &lessons[i]
			break
		}
	}
	require.NotNil(t, vibora)
	assert.Equal(t, "The Víbora", vibora.Title)
	assert.Equal(t, "the-vibora", Slugify(vibora.Title))
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"The Continental Grip", "the-continental-grip"},
		{"The Víbora", "the-vibora"},
		{"Chassé Footwork", "chasse-footwork"},
		{"Por Tres & Por Cuatro — The Exit Smashes", "por-tres-por-cuatro-the-exit-smashes"},
		{"Bajada Smash — The Cuchilla", "bajada-smash-the-cuchilla"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, Slugify(c.in), c.in)
	}
}

// lessonSource builds one syntactically complete lesson so the parser can be
// exercised without the curriculum file, which lives outside the repo.
func lessonSource(mistakeLine string) string {
	return strings.Join([]string{
		"**LESSON 1 · BEGINNER**",
		"Title: The Continental Grip",
		`Tagline: "Hold it like a hammer."`,
		"Focus: Establish the one grip.",
		"Watch For:",
		"    0:3 — V between thumb and index finger",
		"    0:7 — Wrist stays neutral",
		"    0:11 — Light pressure",
		mistakeLine,
		"Drill: Grip Freeze · 5 min · Recommended · Freeze and check the V.",
	}, "\n")
}

// The percentage in "Common Mistake (62% of beginners):" is no longer stored,
// so the parser must read the line with or without it — today's curriculum
// still carries one, and a rewritten one need not.
func TestParseCurriculum_MistakeLineWithOrWithoutAPercentage(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"with a percentage", "Common Mistake (62% of beginners): Sliding into a frying-pan grip."},
		{"with a parenthetical but no number", "Common Mistake (most beginners): Sliding into a frying-pan grip."},
		{"with no parenthetical at all", "Common Mistake: Sliding into a frying-pan grip."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lessons, err := Parse(strings.NewReader(lessonSource(tt.line)))
			require.NoError(t, err)
			require.Len(t, lessons, 1)
			assert.Equal(t, "Sliding into a frying-pan grip.", lessons[0].Mistake.Text)
		})
	}
}

// A mistake line is still required — dropping the statistic must not make the
// coaching text optional too.
func TestParseCurriculum_StillRequiresTheMistakeText(t *testing.T) {
	src := strings.ReplaceAll(lessonSource("Common Mistake: x"), "Common Mistake: x\n", "")
	_, err := Parse(strings.NewReader(src))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing common mistake")
}

package achievements

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// get returns the achievement with the given slug, failing the test if the
// summary doesn't contain it.
func get(t *testing.T, s Summary, slug string) Achievement {
	t.Helper()
	for _, a := range s.Achievements {
		if a.Slug == slug {
			return a
		}
	}
	t.Fatalf("achievement %q not in summary", slug)
	return Achievement{}
}

// at builds a deterministic UTC timestamp h hours after a fixed base.
func at(h int) *time.Time {
	v := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC).Add(time.Duration(h) * time.Hour)
	return &v
}

// rfc is the wire format compute emits for unlock timestamps.
func rfc(t *time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func TestCompute_ZeroState(t *testing.T) {
	got := compute(userStats{})

	assert.Equal(t, 0, got.Unlocked)
	assert.Equal(t, 10, got.Total)
	require.Len(t, got.Achievements, 10)

	// Grid order is the catalogue order — the client renders top-to-bottom.
	wantOrder := []string{
		"first-lesson", "first-win", "match-diarist", "bandeja-apprentice",
		"marcos-curious", "comeback-kid", "padel-license", "vibora-master",
		"win-streak-3", "half-mastery",
	}
	for i, a := range got.Achievements {
		assert.Equal(t, wantOrder[i], a.Slug, "achievement %d out of order", i)
		assert.False(t, a.Unlocked, "%s must be locked for a fresh user", a.Slug)
		assert.Zero(t, a.Progress, "%s progress must be 0", a.Slug)
		assert.Nil(t, a.UnlockedAt, "%s unlocked_at must be null", a.Slug)
	}

	wantLabels := map[string]string{
		"first-lesson":       "0 lessons learned",
		"first-win":          "0 wins logged",
		"match-diarist":      "0 / 10 matches logged",
		"bandeja-apprentice": "Not started yet",
		"marcos-curious":     "0 / 10 messages sent",
		"comeback-kid":       "No comebacks yet",
		"padel-license":      "Exam not attempted yet",
		"vibora-master":      "Not started yet",
		"win-streak-3":       "Longest streak · 0",
		"half-mastery":       "No lessons published",
	}
	for slug, want := range wantLabels {
		assert.Equal(t, want, get(t, got, slug).ProgressLabel, slug)
	}
}

func TestCompute_FirstLessonLearned(t *testing.T) {
	ts := at(0)
	got := compute(userStats{
		HasLearnedLesson:     true,
		LearnedCount:         1,
		FirstLessonAt:        ts,
		PublishedLessonCount: 12,
	})

	a := get(t, got, "first-lesson")
	assert.True(t, a.Unlocked)
	assert.Equal(t, 1, a.Progress)
	assert.Equal(t, 1, a.Target)
	require.NotNil(t, a.UnlockedAt)
	assert.Equal(t, rfc(ts), *a.UnlockedAt)
	assert.Equal(t, "1 lesson learned", a.ProgressLabel)
	assert.Equal(t, 1, got.Unlocked)
}

func TestCompute_HalfMastery(t *testing.T) {
	tests := []struct {
		name         string
		published    int
		mastered     int
		wantUnlocked bool
		wantLabel    string
	}{
		// PublishedLessonCount == 0 never unlocks, even with masteries — the
		// rule requires a positive denominator.
		{"no published lessons", 0, 3, false, "No lessons published"},
		{"zero mastered", 10, 0, false, "0 / 10 mastered · 0%"},
		{"just below half", 10, 4, false, "4 / 10 mastered · 40%"},
		{"exactly half", 10, 5, true, "5 / 10 mastered · 50%"},
		{"above half", 10, 8, true, "8 / 10 mastered · 80%"},
		// Odd count: MasteredCount*2 >= PublishedLessonCount rounds the
		// threshold up (3*2=6 >= 5), so 3 of 5 unlocks but 2 of 5 doesn't.
		{"odd count, ceil reached", 5, 3, true, "3 / 5 mastered · 60%"},
		{"odd count, below ceil", 5, 2, false, "2 / 5 mastered · 40%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(userStats{
				PublishedLessonCount: tt.published,
				MasteredCount:        tt.mastered,
			})
			a := get(t, got, "half-mastery")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
			if tt.wantUnlocked {
				assert.Equal(t, 1, a.Progress)
			} else {
				assert.Zero(t, a.Progress)
			}
		})
	}
}

func TestCompute_HalfMasteryTimestampIsGatherApproximation(t *testing.T) {
	// gather() approximates HalfMasteryAt as the most recent mastery time when
	// the user crosses the threshold (not the exact crossing moment). compute
	// just formats whatever the gather side supplied.
	ts := at(7)
	got := compute(userStats{
		PublishedLessonCount: 4,
		MasteredCount:        2,
		HalfMasteryAt:        ts,
	})
	a := get(t, got, "half-mastery")
	require.True(t, a.Unlocked)
	require.NotNil(t, a.UnlockedAt)
	assert.Equal(t, rfc(ts), *a.UnlockedAt)
}

func TestCompute_BandejaStatusLadder(t *testing.T) {
	tests := []struct {
		status       string
		wantUnlocked bool
		wantLabel    string
	}{
		{"", false, "Not started yet"},
		{"viewed", false, "Currently · Viewed"},
		{"learned", true, "Currently · Learned"},
		{"mastered", true, "Currently · Mastered"},
	}

	for _, tt := range tests {
		t.Run("status "+tt.status, func(t *testing.T) {
			got := compute(userStats{BandejaStatus: tt.status})
			a := get(t, got, "bandeja-apprentice")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}

	t.Run("unlock timestamp flows through", func(t *testing.T) {
		ts := at(1)
		got := compute(userStats{BandejaStatus: "learned", BandejaApprenticeAt: ts})
		a := get(t, got, "bandeja-apprentice")
		require.NotNil(t, a.UnlockedAt)
		assert.Equal(t, rfc(ts), *a.UnlockedAt)
	})
}

func TestCompute_ViboraStatusLadder(t *testing.T) {
	tests := []struct {
		status       string
		wantUnlocked bool
		wantLabel    string
	}{
		{"", false, "Not started yet"},
		{"viewed", false, "Currently · Viewed"},
		// Vibora requires full mastery — "learned" is not enough.
		{"learned", false, "Currently · Learned"},
		{"mastered", true, "Currently · Mastered"},
	}

	for _, tt := range tests {
		t.Run("status "+tt.status, func(t *testing.T) {
			got := compute(userStats{ViboraStatus: tt.status})
			a := get(t, got, "vibora-master")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}
}

func TestCompute_FirstWin(t *testing.T) {
	ts := at(2)
	got := compute(userStats{WinCount: 1, FirstWinAt: ts})

	a := get(t, got, "first-win")
	assert.True(t, a.Unlocked)
	assert.Equal(t, 1, a.Progress)
	assert.Equal(t, "1 win logged", a.ProgressLabel)
	require.NotNil(t, a.UnlockedAt)
	assert.Equal(t, rfc(ts), *a.UnlockedAt)

	locked := get(t, compute(userStats{}), "first-win")
	assert.False(t, locked.Unlocked)
	assert.Equal(t, "0 wins logged", locked.ProgressLabel)
}

func TestCompute_WinStreakThree(t *testing.T) {
	tests := []struct {
		name         string
		streak       int
		wantUnlocked bool
		wantProgress int
		wantLabel    string
	}{
		{"no wins", 0, false, 0, "Longest streak · 0"},
		{"two in a row", 2, false, 2, "Longest streak · 2"},
		{"exactly three", 3, true, 3, "Longest streak · 3"},
		// Progress is clamped to the target but the label shows the real streak.
		{"streak of five", 5, true, 3, "Longest streak · 5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(userStats{LongestWinStreak: tt.streak})
			a := get(t, got, "win-streak-3")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantProgress, a.Progress)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}
}

func TestCompute_ComebackKid(t *testing.T) {
	// The ">= 2 consecutive losses then a win" detection lives in
	// fillMatchStats (the gather side); compute only consumes the HadComeback
	// flag and ComebackAt timestamp it produces.
	ts := at(3)
	got := compute(userStats{HadComeback: true, ComebackAt: ts})

	a := get(t, got, "comeback-kid")
	assert.True(t, a.Unlocked)
	assert.Equal(t, 1, a.Progress)
	assert.Equal(t, "Came back after two losses", a.ProgressLabel)
	require.NotNil(t, a.UnlockedAt)
	assert.Equal(t, rfc(ts), *a.UnlockedAt)

	locked := get(t, compute(userStats{}), "comeback-kid")
	assert.False(t, locked.Unlocked)
	assert.Equal(t, "No comebacks yet", locked.ProgressLabel)
}

func TestCompute_MatchDiarist(t *testing.T) {
	tests := []struct {
		name         string
		matches      int
		wantUnlocked bool
		wantProgress int
		wantLabel    string
	}{
		{"none", 0, false, 0, "0 / 10 matches logged"},
		{"nine", 9, false, 9, "9 / 10 matches logged"},
		{"exactly ten", 10, true, 10, "10 / 10 matches logged"},
		{"fifteen clamps to target", 15, true, 10, "10 / 10 matches logged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(userStats{MatchCount: tt.matches})
			a := get(t, got, "match-diarist")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantProgress, a.Progress)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}
}

func TestCompute_MarcosCurious(t *testing.T) {
	tests := []struct {
		name         string
		messages     int
		wantUnlocked bool
		wantProgress int
		wantLabel    string
	}{
		{"none", 0, false, 0, "0 / 10 messages sent"},
		{"nine", 9, false, 9, "9 / 10 messages sent"},
		{"exactly ten", 10, true, 10, "10 / 10 messages sent"},
		{"twenty five clamps", 25, true, 10, "10 / 10 messages sent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(userStats{UserMessageCount: tt.messages})
			a := get(t, got, "marcos-curious")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantProgress, a.Progress)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}
}

func TestCompute_PadelLicense(t *testing.T) {
	tests := []struct {
		name         string
		stats        userStats
		wantUnlocked bool
		wantLabel    string
	}{
		{
			name:      "never attempted",
			stats:     userStats{},
			wantLabel: "Exam not attempted yet",
		},
		{
			name:      "failed attempt shows latest score",
			stats:     userStats{HasExamAttempt: true, LatestExamScore: 15, LatestExamTotal: 20},
			wantLabel: "15 / 20 correct · 75%",
		},
		{
			name:         "passed",
			stats:        userStats{PassedExam: true, HasExamAttempt: true, LatestExamScore: 18, LatestExamTotal: 20},
			wantUnlocked: true,
			wantLabel:    "18 / 20 correct · 90%",
		},
		{
			// Latest attempt drives the label even after a pass: retaking and
			// failing shows the newer (worse) score while staying unlocked.
			name:         "passed earlier, latest retake failed",
			stats:        userStats{PassedExam: true, HasExamAttempt: true, LatestExamScore: 12, LatestExamTotal: 20},
			wantUnlocked: true,
			wantLabel:    "12 / 20 correct · 60%",
		},
		{
			name:      "attempt with zero total is treated as not attempted",
			stats:     userStats{HasExamAttempt: true, LatestExamTotal: 0},
			wantLabel: "Exam not attempted yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(tt.stats)
			a := get(t, got, "padel-license")
			assert.Equal(t, tt.wantUnlocked, a.Unlocked)
			assert.Equal(t, tt.wantLabel, a.ProgressLabel)
		})
	}

	t.Run("unlock timestamp flows through", func(t *testing.T) {
		ts := at(4)
		got := compute(userStats{PassedExam: true, HasExamAttempt: true, LatestExamScore: 19, LatestExamTotal: 20, ExamPassedAt: ts})
		a := get(t, got, "padel-license")
		require.NotNil(t, a.UnlockedAt)
		assert.Equal(t, rfc(ts), *a.UnlockedAt)
	})
}

func TestCompute_UnlockedAtFlowsThroughForEveryBadge(t *testing.T) {
	stats := userStats{
		HasLearnedLesson:     true,
		LearnedCount:         12,
		MasteredCount:        6,
		PublishedLessonCount: 12,
		BandejaStatus:        "learned",
		ViboraStatus:         "mastered",
		MatchCount:           10,
		WinCount:             4,
		LongestWinStreak:     3,
		HadComeback:          true,
		UserMessageCount:     10,
		PassedExam:           true,
		HasExamAttempt:       true,
		LatestExamScore:      18,
		LatestExamTotal:      20,

		FirstLessonAt:       at(1),
		FirstWinAt:          at(2),
		MatchDiaristAt:      at(3),
		BandejaApprenticeAt: at(4),
		MarcosCuriousAt:     at(5),
		ComebackAt:          at(6),
		ExamPassedAt:        at(7),
		ViboraMasteredAt:    at(8),
		WinStreakThreeAt:    at(9),
		HalfMasteryAt:       at(10),
	}

	got := compute(stats)
	assert.Equal(t, 10, got.Unlocked, "every badge unlocks with these stats")

	want := map[string]*time.Time{
		"first-lesson":       stats.FirstLessonAt,
		"first-win":          stats.FirstWinAt,
		"match-diarist":      stats.MatchDiaristAt,
		"bandeja-apprentice": stats.BandejaApprenticeAt,
		"marcos-curious":     stats.MarcosCuriousAt,
		"comeback-kid":       stats.ComebackAt,
		"padel-license":      stats.ExamPassedAt,
		"vibora-master":      stats.ViboraMasteredAt,
		"win-streak-3":       stats.WinStreakThreeAt,
		"half-mastery":       stats.HalfMasteryAt,
	}
	for slug, ts := range want {
		a := get(t, got, slug)
		require.True(t, a.Unlocked, slug)
		require.NotNil(t, a.UnlockedAt, slug)
		assert.Equal(t, rfc(ts), *a.UnlockedAt, slug)
	}
}

func TestCompute_UnlockedAtIsNormalisedToUTC(t *testing.T) {
	// formatTime converts to UTC before RFC3339-formatting, so a +02:00 DB
	// timestamp serialises with a Z suffix.
	cest := time.FixedZone("CEST", 2*3600)
	local := time.Date(2026, 6, 1, 12, 30, 0, 0, cest)
	got := compute(userStats{WinCount: 1, FirstWinAt: &local})

	a := get(t, got, "first-win")
	require.NotNil(t, a.UnlockedAt)
	assert.Equal(t, "2026-06-01T10:30:00Z", *a.UnlockedAt)
}

// The bare "%d lessons"/"%d wins" formats read wrong at exactly the count these
// achievements unlock on, which is when a player is most likely to open them.
func TestCompute_CountLabelsUseSingularForOne(t *testing.T) {
	tests := []struct {
		name  string
		stats userStats
		slug  string
		want  string
	}{
		{"zero lessons stays plural", userStats{LearnedCount: 0, PublishedLessonCount: 12}, "first-lesson", "0 lessons learned"},
		{"one lesson is singular", userStats{HasLearnedLesson: true, LearnedCount: 1, PublishedLessonCount: 12}, "first-lesson", "1 lesson learned"},
		{"two lessons is plural", userStats{HasLearnedLesson: true, LearnedCount: 2, PublishedLessonCount: 12}, "first-lesson", "2 lessons learned"},
		{"zero wins stays plural", userStats{WinCount: 0}, "first-win", "0 wins logged"},
		{"one win is singular", userStats{WinCount: 1}, "first-win", "1 win logged"},
		{"three wins is plural", userStats{WinCount: 3}, "first-win", "3 wins logged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compute(tt.stats)
			assert.Equal(t, tt.want, get(t, got, tt.slug).ProgressLabel)
		})
	}
}

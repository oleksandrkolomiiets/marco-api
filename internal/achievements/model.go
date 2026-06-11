package achievements

import (
	"fmt"
	"time"
)

// Achievement is one badge in the user's profile grid. It's a derived view —
// the unlocked state is recomputed on every read from lessons, match logs,
// exam attempts and chat messages. No achievements table exists.
type Achievement struct {
	Slug          string  `json:"slug"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Criteria      string  `json:"criteria"`
	ProgressLabel string  `json:"progress_label"`
	Icon          string  `json:"icon"`
	Accent        string  `json:"accent"` // "teal" | "orange" | "ink"
	Unlocked      bool    `json:"unlocked"`
	Progress      int     `json:"progress"`
	Target        int     `json:"target"`
	UnlockedAt    *string `json:"unlocked_at"`
}

// Summary is what the handler returns. Definitions stay server-side; the
// client just renders what we send.
type Summary struct {
	Unlocked     int           `json:"unlocked"`
	Total        int           `json:"total"`
	Achievements []Achievement `json:"achievements"`
}

// definition is the static catalogue entry behind one achievement. Description
// is the flavor copy shown in the detail sheet; criteria is the literal
// "how to earn" instruction.
type definition struct {
	Slug        string
	Title       string
	Description string
	Criteria    string
	Icon        string
	Accent      string // teal (default brand), orange (warm accent), ink (dark)
	Target      int
}

// Catalogue is the canonical ordered list of achievements shown on the
// profile. Order matters — the grid renders them top-to-bottom in this order.
var catalogue = []definition{
	{
		Slug:        "first-lesson",
		Title:       "First lesson",
		Description: "Every coach has a first day. Watch one lesson end to end and you're already further than most.",
		Criteria:    "Mark any lesson as Learned or Mastered.",
		Icon:        "▸",
		Accent:      "teal",
		Target:      1,
	},
	{
		Slug:        "first-win",
		Title:       "First win logged",
		Description: "The first W is the one you remember. Log it so we can find the pattern when the next ones come.",
		Criteria:    "Log a match with result · Won.",
		Icon:        "W",
		Accent:      "teal",
		Target:      1,
	},
	{
		Slug:        "match-diarist",
		Title:       "Match diarist",
		Description: "Ten logged matches is enough data for Marco to spot what's actually moving your win rate.",
		Criteria:    "Log ten matches in your diary.",
		Icon:        "10",
		Accent:      "teal",
		Target:      10,
	},
	{
		Slug:        "bandeja-apprentice",
		Title:       "Bandeja apprentice",
		Description: "The bandeja is the shot that keeps you at the net. Get it under control and the court shrinks for your opponents.",
		Criteria:    "Mark the bandeja lesson as Learned.",
		Icon:        "B",
		Accent:      "orange",
		Target:      1,
	},
	{
		Slug:        "marcos-curious",
		Title:       "Marco's curious",
		Description: "Ten questions in. You're not just playing — you're asking why the ball does what it does.",
		Criteria:    "Send ten messages to Marco.",
		Icon:        "?",
		Accent:      "orange",
		Target:      10,
	},
	{
		Slug:        "comeback-kid",
		Title:       "Comeback kid",
		Description: "Two losses, then a win. The streak ends when you decide it does.",
		Criteria:    "Log a win after two losses in a row.",
		Icon:        "↺",
		Accent:      "ink",
		Target:      1,
	},
	{
		Slug:        "padel-license",
		Title:       "Padel rules license",
		Description: "You scored above 80% on the padel rules exam. Officially: you know what a let, a fault, and a double-bounce off the wall actually mean.",
		Criteria:    "Answer >80% of rules exam questions correctly.",
		Icon:        "L1",
		Accent:      "teal",
		Target:      1,
	},
	{
		Slug:        "vibora-master",
		Title:       "Vibora master",
		Description: "Master the vibora — the shot that turns defense into offense at the net.",
		Criteria:    "Mark the Vibora lesson as Mastered.",
		Icon:        "V",
		Accent:      "teal",
		Target:      1,
	},
	{
		Slug:        "win-streak-3",
		Title:       "Win streak ×3",
		Description: "Three wins back to back. Not luck — pattern.",
		Criteria:    "Log three wins in a row.",
		Icon:        "×3",
		Accent:      "teal",
		Target:      3,
	},
	{
		Slug:        "half-mastery",
		Title:       "Half mastery",
		Description: "Half the curriculum, fully owned. The other half is just more reps from here.",
		Criteria:    "Master at least half of every published lesson.",
		Icon:        "½",
		Accent:      "teal",
		Target:      1,
	},
}

// userStats is everything Compute needs from the database. Bundling it keeps
// the store responsible for SQL and Compute responsible for unlock rules.
type userStats struct {
	HasLearnedLesson     bool
	LearnedCount         int
	MasteredCount        int
	PublishedLessonCount int
	BandejaStatus        string // "", "viewed", "learned", "mastered"
	ViboraStatus         string
	MatchCount           int
	WinCount             int
	LongestWinStreak     int
	HadComeback          bool
	UserMessageCount     int
	PassedExam           bool
	LatestExamScore      int
	LatestExamTotal      int
	HasExamAttempt       bool

	FirstLessonAt       *time.Time
	FirstWinAt          *time.Time
	MatchDiaristAt      *time.Time
	BandejaApprenticeAt *time.Time
	MarcosCuriousAt     *time.Time
	ComebackAt          *time.Time
	ExamPassedAt        *time.Time
	ViboraMasteredAt    *time.Time
	WinStreakThreeAt    *time.Time
	HalfMasteryAt       *time.Time

	// lastMasteryAt is used internally as a proxy for the half-mastery unlock
	// timestamp once the user crosses the threshold. Not serialised.
	lastMasteryAt *time.Time
}

// compute applies the unlock rules to a userStats snapshot.
func compute(s userStats) Summary {
	out := make([]Achievement, 0, len(catalogue))
	unlocked := 0
	for _, d := range catalogue {
		a := Achievement{
			Slug:        d.Slug,
			Title:       d.Title,
			Description: d.Description,
			Criteria:    d.Criteria,
			Icon:        d.Icon,
			Accent:      d.Accent,
			Target:      d.Target,
		}
		switch d.Slug {
		case "first-lesson":
			a.Unlocked = s.HasLearnedLesson
			if a.Unlocked {
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.FirstLessonAt)
			a.ProgressLabel = fmt.Sprintf("%d lessons learned", s.LearnedCount)
		case "first-win":
			a.Unlocked = s.WinCount >= 1
			if a.Unlocked {
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.FirstWinAt)
			a.ProgressLabel = fmt.Sprintf("%d wins logged", s.WinCount)
		case "match-diarist":
			a.Progress = clamp(s.MatchCount, d.Target)
			a.Unlocked = s.MatchCount >= d.Target
			a.UnlockedAt = formatTime(s.MatchDiaristAt)
			a.ProgressLabel = fmt.Sprintf("%d / %d matches logged", clamp(s.MatchCount, d.Target), d.Target)
		case "bandeja-apprentice":
			a.Unlocked = s.BandejaStatus == "learned" || s.BandejaStatus == "mastered"
			if a.Unlocked {
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.BandejaApprenticeAt)
			a.ProgressLabel = lessonStatusLabel(s.BandejaStatus)
		case "marcos-curious":
			a.Progress = clamp(s.UserMessageCount, d.Target)
			a.Unlocked = s.UserMessageCount >= d.Target
			a.UnlockedAt = formatTime(s.MarcosCuriousAt)
			a.ProgressLabel = fmt.Sprintf("%d / %d messages sent", clamp(s.UserMessageCount, d.Target), d.Target)
		case "comeback-kid":
			a.Unlocked = s.HadComeback
			if a.Unlocked {
				a.Progress = 1
				a.ProgressLabel = "Came back after two losses"
			} else {
				a.ProgressLabel = "No comebacks yet"
			}
			a.UnlockedAt = formatTime(s.ComebackAt)
		case "padel-license":
			a.Unlocked = s.PassedExam
			if a.Unlocked {
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.ExamPassedAt)
			a.ProgressLabel = examProgressLabel(s)
		case "vibora-master":
			a.Unlocked = s.ViboraStatus == "mastered"
			if a.Unlocked {
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.ViboraMasteredAt)
			a.ProgressLabel = lessonStatusLabel(s.ViboraStatus)
		case "win-streak-3":
			a.Progress = clamp(s.LongestWinStreak, d.Target)
			a.Unlocked = s.LongestWinStreak >= d.Target
			a.UnlockedAt = formatTime(s.WinStreakThreeAt)
			a.ProgressLabel = fmt.Sprintf("Longest streak · %d", s.LongestWinStreak)
		case "half-mastery":
			if s.PublishedLessonCount > 0 && s.MasteredCount*2 >= s.PublishedLessonCount {
				a.Unlocked = true
				a.Progress = 1
			}
			a.UnlockedAt = formatTime(s.HalfMasteryAt)
			a.ProgressLabel = halfMasteryLabel(s)
		}
		if a.Unlocked {
			unlocked++
		}
		out = append(out, a)
	}
	return Summary{Unlocked: unlocked, Total: len(catalogue), Achievements: out}
}

func lessonStatusLabel(status string) string {
	switch status {
	case "viewed":
		return "Currently · Viewed"
	case "learned":
		return "Currently · Learned"
	case "mastered":
		return "Currently · Mastered"
	default:
		return "Not started yet"
	}
}

func examProgressLabel(s userStats) string {
	if !s.HasExamAttempt || s.LatestExamTotal == 0 {
		return "Exam not attempted yet"
	}
	pct := (s.LatestExamScore * 100) / s.LatestExamTotal
	return fmt.Sprintf("%d / %d correct · %d%%", s.LatestExamScore, s.LatestExamTotal, pct)
}

func halfMasteryLabel(s userStats) string {
	if s.PublishedLessonCount == 0 {
		return "No lessons published"
	}
	pct := (s.MasteredCount * 100) / s.PublishedLessonCount
	return fmt.Sprintf("%d / %d mastered · %d%%", s.MasteredCount, s.PublishedLessonCount, pct)
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

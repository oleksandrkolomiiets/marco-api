package marco

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"marco-api/internal/anthropic"
)

const messageHistoryLimit = 30

// UserContext is the structured payload injected into Marco's system prompt.
// The JSON shape is the contract with the model — any change to a JSON tag
// is effectively a prompt change and should bump the prompt version.
type UserContext struct {
	Today                string            `json:"today"` // YYYY-MM-DD, used by Marco as the default played_on date
	User                 UserInfo          `json:"user"`
	Progress             Progress          `json:"progress"`
	Goals                []string          `json:"goals"`
	LastMatch            *LastMatch        `json:"last_match,omitempty"`
	AvailableLessons     []LessonInfo      `json:"available_lessons"`
	UpcomingPreparations []PreparationInfo `json:"upcoming_preparations"`
}

// PreparationInfo is the slim view of a match preparation Marco sees in the
// context block. It is the ONLY valid source for the id field of a
// [MATCH_PREP: {"mode":"adjust", ...}] token — the same rule that governs
// LessonInfo for [LESSON_REF: ...].
type PreparationInfo struct {
	ID          string `json:"id"`
	ScheduledAt string `json:"scheduled_at"` // RFC3339
	// Weekday is scheduled_at's day name, and Day is "today", "tomorrow" or
	// "" — both derived here rather than left to the model.
	//
	// Players refer to preps by day ("adjust Thursday's prep", "add a drill to
	// tomorrow's"), and with only an RFC3339 string in the context that means
	// working out which weekday a date falls on, then comparing. Asked to
	// adjust Thursday's prep with a Friday prep and a Thursday prep on file,
	// Marco picked the Friday one and called it "Thursday's prep against
	// Clara" — the right shape of token pointing at the wrong match, which is
	// worse than refusing, because the tag looks correct until it opens.
	Weekday        string   `json:"weekday"`
	Day            string   `json:"day,omitempty"`
	Opponents      []string `json:"opponents"`
	PartnerName    string   `json:"partner_name,omitempty"`
	Court          string   `json:"court,omitempty"`
	Note           string   `json:"note,omitempty"`
	PreparationPct int      `json:"preparation_pct"`
}

// relativeDay names a prep's date the way the player would: "today" or
// "tomorrow" when it falls there, and otherwise nothing, leaving the weekday
// to carry it. Both dates are compared in UTC, matching the wall-clock-in-UTC
// convention scheduled_at is stored under.
func relativeDay(scheduled, now time.Time) string {
	s := scheduled.UTC()
	n := now.UTC()
	sDay := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC)
	nDay := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	switch sDay.Sub(nDay) {
	case 0:
		return "today"
	case 24 * time.Hour:
		return "tomorrow"
	}
	return ""
}

// LessonInfo is the slug+title+level triple Marco needs to emit a valid
// [LESSON_REF: ...] token. The full curriculum is exposed so Marco can
// recommend lessons the user has not yet engaged with, without inventing
// ids or titles.
type LessonInfo struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Level string `json:"level"`
}

type UserInfo struct {
	Name      string `json:"name"`
	Level     string `json:"level"`
	Hand      string `json:"hand"`
	Side      string `json:"side"`
	Frequency string `json:"frequency"`
	TopGoal   string `json:"top_goal,omitempty"`
	Injury    string `json:"injury,omitempty"`
}

type Progress struct {
	Completed  []string `json:"completed"`
	InProgress string   `json:"in_progress,omitempty"`
	Mastered   []string `json:"mastered"`
}

type LastMatch struct {
	Played  bool   `json:"played"`
	Result  string `json:"result,omitempty"`
	Feeling string `json:"feeling,omitempty"`
	Note    string `json:"note,omitempty"`
}

// ContextJSON returns the UserContext serialised to compact JSON for prompt injection.
func (uc UserContext) ContextJSON() string {
	b, _ := json.Marshal(uc) // marshalling a known struct cannot fail
	return string(b)
}

// Assembler reads from Postgres to build the per-user context block that Marco
// receives in its system prompt, plus the recent conversation history.
type Assembler struct {
	db *pgxpool.Pool
}

func NewAssembler(db *pgxpool.Pool) *Assembler {
	return &Assembler{db: db}
}

// Build returns the user context, the recent message history (oldest first,
// excluding the latest user message which the caller is about to send), and
// any error.
func (a *Assembler) Build(ctx context.Context, userID uuid.UUID) (UserContext, []anthropic.Message, error) {
	now := time.Now()
	uc := UserContext{
		Today:                now.Format("2006-01-02"),
		Goals:                []string{},
		Progress:             Progress{Completed: []string{}, Mastered: []string{}},
		AvailableLessons:     []LessonInfo{},
		UpcomingPreparations: []PreparationInfo{},
	}

	// Build runs on the critical path of every chat turn — its latency delays
	// the first streamed token. The seven loads are independent reads, so run
	// them concurrently; pgxpool hands each goroutine its own connection, and
	// each goroutine writes a distinct field (g.Wait establishes the
	// happens-before for the caller).
	var history []anthropic.Message
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		info, err := a.loadUser(gctx, userID)
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		uc.User = info
		return nil
	})
	g.Go(func() error {
		progress, err := a.loadProgress(gctx, userID)
		if err != nil {
			return fmt.Errorf("load progress: %w", err)
		}
		uc.Progress = progress
		return nil
	})
	g.Go(func() error {
		goals, err := a.loadGoals(gctx, userID)
		if err != nil {
			return fmt.Errorf("load goals: %w", err)
		}
		uc.Goals = goals
		return nil
	})
	g.Go(func() error {
		lastMatch, err := a.loadLastMatch(gctx, userID)
		if err != nil {
			return fmt.Errorf("load last match: %w", err)
		}
		uc.LastMatch = lastMatch
		return nil
	})
	g.Go(func() error {
		available, err := a.loadAvailableLessons(gctx)
		if err != nil {
			return fmt.Errorf("load available lessons: %w", err)
		}
		uc.AvailableLessons = available
		return nil
	})
	g.Go(func() error {
		preps, err := a.loadUpcomingPreparations(gctx, userID, now)
		if err != nil {
			return fmt.Errorf("load upcoming preparations: %w", err)
		}
		uc.UpcomingPreparations = preps
		return nil
	})
	g.Go(func() error {
		h, err := a.loadHistory(gctx, userID)
		if err != nil {
			return fmt.Errorf("load history: %w", err)
		}
		history = h
		return nil
	})
	if err := g.Wait(); err != nil {
		return UserContext{}, nil, err
	}

	return uc, history, nil
}

func (a *Assembler) loadUser(ctx context.Context, userID uuid.UUID) (UserInfo, error) {
	const q = `
		SELECT
			COALESCE(display_name, ''),
			COALESCE(skill_level, ''),
			COALESCE(dominant_hand, ''),
			COALESCE(court_side, ''),
			COALESCE(play_frequency, ''),
			COALESCE(goal, ''),
			COALESCE(injury_notes, '')
		FROM users
		WHERE id = $1
	`
	var info UserInfo
	err := a.db.QueryRow(ctx, q, userID).Scan(
		&info.Name,
		&info.Level,
		&info.Hand,
		&info.Side,
		&info.Frequency,
		&info.TopGoal,
		&info.Injury,
	)
	if err != nil {
		return UserInfo{}, err
	}
	return info, nil
}

func (a *Assembler) loadProgress(ctx context.Context, userID uuid.UUID) (Progress, error) {
	const q = `
		SELECT l.slug, ulp.status
		FROM user_lesson_progress ulp
		JOIN lessons l ON l.id = ulp.lesson_id
		WHERE ulp.user_id = $1
		ORDER BY ulp.updated_at DESC
	`
	rows, err := a.db.Query(ctx, q, userID)
	if err != nil {
		return Progress{}, err
	}
	defer rows.Close()

	p := Progress{Completed: []string{}, Mastered: []string{}}
	for rows.Next() {
		var slug, status string
		if err := rows.Scan(&slug, &status); err != nil {
			return Progress{}, err
		}
		switch status {
		case "learned":
			p.Completed = append(p.Completed, slug)
		case "mastered":
			p.Mastered = append(p.Mastered, slug)
		case "viewed":
			if p.InProgress == "" {
				p.InProgress = slug
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Progress{}, err
	}
	return p, nil
}

func (a *Assembler) loadGoals(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const q = `
		SELECT description
		FROM goals
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at ASC
		LIMIT 3
	`
	rows, err := a.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	goals := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		goals = append(goals, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return goals, nil
}

// loadAvailableLessons returns the full published curriculum so Marco can
// recommend lessons the user has not yet engaged with. The list is ordered
// by level then order_index — beginners first, then intermediates, then
// advanced — so Marco can reach for level-appropriate lessons easily.
//
// At current scale (single-digit lessons) we send the whole curriculum.
// When the catalog grows past ~30 lessons we should filter by user level
// here to keep prompt tokens bounded.
func (a *Assembler) loadAvailableLessons(ctx context.Context) ([]LessonInfo, error) {
	const q = `
		SELECT slug, title, level
		FROM lessons
		WHERE published = true
		ORDER BY
			CASE level
				WHEN 'beginner' THEN 1
				WHEN 'intermediate' THEN 2
				WHEN 'advanced' THEN 3
				ELSE 4
			END,
			order_index
	`
	rows, err := a.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lessons := []LessonInfo{}
	for rows.Next() {
		var li LessonInfo
		if err := rows.Scan(&li.Slug, &li.Title, &li.Level); err != nil {
			return nil, err
		}
		lessons = append(lessons, li)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lessons, nil
}

func (a *Assembler) loadLastMatch(ctx context.Context, userID uuid.UUID) (*LastMatch, error) {
	const q = `
		SELECT played, COALESCE(result, ''), COALESCE(feeling, ''), COALESCE(note, '')
		FROM match_logs
		WHERE user_id = $1
		ORDER BY played_on DESC
		LIMIT 1
	`
	var m LastMatch
	err := a.db.QueryRow(ctx, q, userID).Scan(&m.Played, &m.Result, &m.Feeling, &m.Note)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// loadUpcomingPreparations returns the user's preparations that are still
// adjustable from chat: not yet played and either scheduled in the future or
// scheduled within the last 24h (so a player can still tweak their queue
// after the match has technically started). Capped at 5 rows to keep prompt
// tokens bounded; ordered soonest-first so "Thursday's prep" resolves
// naturally to whichever fixture is closest.
func (a *Assembler) loadUpcomingPreparations(ctx context.Context, userID uuid.UUID, now time.Time) ([]PreparationInfo, error) {
	const q = `
		SELECT id, scheduled_at, opponents, COALESCE(partner_name, ''), COALESCE(court, ''), COALESCE(note, ''),
		       COALESCE(
		           (
		               SELECT (SUM(CASE WHEN completed THEN 1 ELSE 0 END) * 100 + COUNT(*) / 2) / NULLIF(COUNT(*), 0)
		               FROM match_preparation_drills d
		               WHERE d.match_preparation_id = mp.id
		           ),
		           0
		       )::int AS preparation_pct
		FROM match_preparation mp
		WHERE user_id = $1
		  AND played_at IS NULL
		  AND scheduled_at >= NOW() - INTERVAL '24 hours'
		ORDER BY scheduled_at ASC
		LIMIT 5
	`
	rows, err := a.db.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	preps := []PreparationInfo{}
	for rows.Next() {
		var (
			id        uuid.UUID
			scheduled time.Time
			info      PreparationInfo
		)
		if err := rows.Scan(&id, &scheduled, &info.Opponents, &info.PartnerName, &info.Court, &info.Note, &info.PreparationPct); err != nil {
			return nil, err
		}
		info.ID = id.String()
		info.ScheduledAt = scheduled.UTC().Format(time.RFC3339)
		info.Weekday = scheduled.UTC().Weekday().String()
		info.Day = relativeDay(scheduled, now)
		if info.Opponents == nil {
			info.Opponents = []string{}
		}
		preps = append(preps, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return preps, nil
}

func (a *Assembler) loadHistory(ctx context.Context, userID uuid.UUID) ([]anthropic.Message, error) {
	const q = `
		SELECT role, content
		FROM messages
		WHERE user_id = $1 AND role IN ('user', 'assistant')
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := a.db.Query(ctx, q, userID, messageHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect newest-first, then reverse below so the caller gets oldest-first.
	var reversed []anthropic.Message
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, err
		}
		var r anthropic.Role
		switch role {
		case "user":
			r = anthropic.RoleUser
		case "assistant":
			r = anthropic.RoleAssistant
		default:
			continue
		}
		reversed = append(reversed, anthropic.Message{Role: r, Content: content})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	msgs := make([]anthropic.Message, len(reversed))
	for i, m := range reversed {
		msgs[len(reversed)-1-i] = m
	}
	if msgs == nil {
		msgs = []anthropic.Message{}
	}
	return msgs, nil
}

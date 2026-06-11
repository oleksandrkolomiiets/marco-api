package match_preparation

import (
	"time"

	"github.com/google/uuid"
)

// Drill is one queued prep item — a named exercise with a target duration and a
// done flag. Preparation % is derived from completed/total drills, so the queue
// is the single source of truth for "how prepared is this player for this match."
type Drill struct {
	ID              uuid.UUID `json:"id"`
	Position        int       `json:"position"`
	Title           string    `json:"title"`
	DurationSeconds int       `json:"duration_seconds"`
	Completed       bool      `json:"completed"`
	CreatedAt       time.Time `json:"created_at"`
}

// Preparation is a prep diary entry tied to one upcoming (or just-played) match.
// MatchLogID is set once the user logs the actual match, and PlanGrade ("worked",
// "mixed", or "missed") is the player's after-the-fact judgement of whether the
// queue paid off — the link between prep and result that gives Marco a coaching
// signal.
//
// PlayedAt carries the explicit "I played this" signal. The client toggles it
// from the preparation sheet so a prep can be marked done before its scheduled
// time or stay open after it — separate from the auto-derived past/upcoming
// split the list view uses.
type Preparation struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	MatchLogID *uuid.UUID `json:"match_log_id"`
	// MessageID points back at the assistant chat message that spawned this
	// prep (set by the chat "Set up prep" / "Adjust prep" tag). Null for preps
	// created from the Match Prep tab. Used by the chat UI to render the
	// "Prep ready" pill across app restarts — same role as match_logs.message_id.
	MessageID      *uuid.UUID `json:"message_id"`
	ScheduledAt    time.Time  `json:"scheduled_at"`
	PlayedAt       *time.Time `json:"played_at"`
	Opponents      []string   `json:"opponents"`
	PartnerName    *string    `json:"partner_name"`
	Court          *string    `json:"court"`
	Note           *string    `json:"note"`
	PlanGrade      *string    `json:"plan_grade"`
	PreparationPct int        `json:"preparation_pct"`
	Drills         []Drill    `json:"drills"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// computePreparationPct rounds completed/total to the nearest whole percent.
// An empty queue is 0% — distinct from "no drills planned yet" the caller can
// detect via len(Drills) == 0.
func computePreparationPct(drills []Drill) int {
	if len(drills) == 0 {
		return 0
	}
	done := 0
	for _, d := range drills {
		if d.Completed {
			done++
		}
	}
	return (done*100 + len(drills)/2) / len(drills)
}

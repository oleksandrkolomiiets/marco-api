package chat

import (
	"time"

	"github.com/google/uuid"
	"marco-api/internal/marco"
)

type Message struct {
	ID            uuid.UUID         `json:"id"`
	Role          string            `json:"role"`
	Content       string            `json:"content"`
	LessonRefs    []marco.LessonRef `json:"lesson_refs"`
	FeedbackScore int8              `json:"feedback_score,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	// MatchLogPrefill is the parsed [MATCH_LOG: ...] token from the assistant
	// message, if present. Nil when the message does not invite logging a match.
	MatchLogPrefill *marco.MatchLogToken `json:"match_log_prefill,omitempty"`
	// MatchLogged is true when a match_logs row exists linking back to this
	// message — the chat UI renders the "Logged" badge instead of the action tag.
	MatchLogged bool `json:"match_logged,omitempty"`
	// MatchPrepPrefill is the parsed [MATCH_PREP: ...] token from the assistant
	// message, if present. Tells the chat UI to render the "Adjust prep" /
	// "Set up prep" action tag, which opens the prep sliding sheet.
	MatchPrepPrefill *marco.MatchPrepToken `json:"match_prep_prefill,omitempty"`
	// MatchPreparationID is the id of the match_preparation row spawned by this
	// message, if the user has already tapped the "Set up prep" tag. Lets the
	// chat UI render the "Prep ready" state across app restarts and short-
	// circuit re-taps to open the existing prep instead of creating a duplicate.
	MatchPreparationID *uuid.UUID `json:"match_preparation_id,omitempty"`
}

package exam

import (
	"time"

	"github.com/google/uuid"
)

// Question is one of the 20 rules-exam questions. Options are inlined.
type Question struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	OrderIndex  int       `json:"order_index"`
	Category    string    `json:"category"`
	Prompt      string    `json:"prompt"`
	Explanation *string   `json:"explanation"`
	Options     []Option  `json:"options"`
}

// Option is one answer choice on a Question. IsCorrect is only populated on
// review responses — it stays false in the take-the-exam payload so the client
// can't cheat.
type Option struct {
	ID         uuid.UUID `json:"id"`
	OrderIndex int       `json:"order_index"`
	Text       string    `json:"text"`
	IsCorrect  bool      `json:"is_correct,omitempty"`
}

// Attempt is the high-level result of one exam run.
type Attempt struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Score       int       `json:"score"`
	Total       int       `json:"total"`
	Passed      bool      `json:"passed"`
	CompletedAt time.Time `json:"completed_at"`
}

// AttemptAnswer is what the user picked for a single question on an attempt.
// SelectedOptionID is nullable because we accept unanswered questions (the
// frontend can submit them as wrong without penalty to the flow).
type AttemptAnswer struct {
	QuestionID       uuid.UUID  `json:"question_id"`
	SelectedOptionID *uuid.UUID `json:"selected_option_id"`
	IsCorrect        bool       `json:"is_correct"`
}

// AttemptReview is the post-submit payload: the attempt summary, every question
// with its options including which were correct, and the user's pick per
// question. Renders the Results screen on the client.
type AttemptReview struct {
	Attempt
	Questions []QuestionReview `json:"questions"`
}

type QuestionReview struct {
	Question
	SelectedOptionID *uuid.UUID `json:"selected_option_id"`
	CorrectOptionID  uuid.UUID  `json:"correct_option_id"`
	IsCorrect        bool       `json:"is_correct"`
}

// PassingScore is the threshold at which an attempt is marked passed.
const PassingScore = 18

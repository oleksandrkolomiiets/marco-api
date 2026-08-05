package exam

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrPickNotOnQuestion means the client submitted an option id that belongs to
// a different question. It's bad input, not a server fault, so the handler
// answers 422 rather than letting it fall through to a 500.
var ErrPickNotOnQuestion = errors.New("selected option does not belong to the question")

// gradedAnswer is the outcome of grading a single question on an attempt.
// selectedOptionID is nil when the question was left unanswered.
type gradedAnswer struct {
	questionID       uuid.UUID
	selectedOptionID *uuid.UUID
	correctOptionID  uuid.UUID
	isCorrect        bool
}

// gradeResult is the full outcome of grading one attempt. answers is ordered
// exactly like the questions slice passed to gradeAttempt.
type gradeResult struct {
	answers []gradedAnswer
	score   int
	total   int
	passed  bool
}

// gradeAttempt is the pure grading computation behind SubmitAttempt. picks
// maps question_id → selected_option_id; questions missing from picks are
// graded wrong with a nil selection. Picks keyed by a question id that isn't
// in questions are ignored, but a pick whose option does not belong to its
// question is an error. passed is score >= PassingScore.
func gradeAttempt(questions []Question, picks map[uuid.UUID]uuid.UUID) (gradeResult, error) {
	answers := make([]gradedAnswer, 0, len(questions))
	score := 0

	for _, q := range questions {
		var correctID uuid.UUID
		for _, o := range q.Options {
			if o.IsCorrect {
				correctID = o.ID
				break
			}
		}
		picked, has := picks[q.ID]
		var sel *uuid.UUID
		isCorrect := false
		if has {
			// Reject picks that don't belong to this question.
			belongs := false
			for _, o := range q.Options {
				if o.ID == picked {
					belongs = true
					break
				}
			}
			if !belongs {
				return gradeResult{}, fmt.Errorf("%w: option %s, question %s", ErrPickNotOnQuestion, picked, q.ID)
			}
			sel = &picked
			isCorrect = picked == correctID
		}
		if isCorrect {
			score++
		}
		answers = append(answers, gradedAnswer{
			questionID:       q.ID,
			selectedOptionID: sel,
			correctOptionID:  correctID,
			isCorrect:        isCorrect,
		})
	}

	return gradeResult{
		answers: answers,
		score:   score,
		total:   len(questions),
		passed:  score >= PassingScore,
	}, nil
}

// questionReviews zips questions with their graded answers into the
// per-question review payload. answers must be ordered like questions, as
// returned by gradeAttempt.
func questionReviews(questions []Question, answers []gradedAnswer) []QuestionReview {
	out := make([]QuestionReview, 0, len(questions))
	for i, q := range questions {
		ga := answers[i]
		out = append(out, QuestionReview{
			Question:         q,
			SelectedOptionID: ga.selectedOptionID,
			CorrectOptionID:  ga.correctOptionID,
			IsCorrect:        ga.isCorrect,
		})
	}
	return out
}

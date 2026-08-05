package exam

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeExam builds n questions with three options each. Options[0] is always
// the correct one, mirroring GetQuestionsForReview output (IsCorrect set).
func makeExam(n int) []Question {
	qs := make([]Question, 0, n)
	for i := 0; i < n; i++ {
		qs = append(qs, Question{
			ID:         uuid.New(),
			Slug:       fmt.Sprintf("q-%d", i+1),
			OrderIndex: i + 1,
			Category:   "rules",
			Prompt:     fmt.Sprintf("Question %d?", i+1),
			Options: []Option{
				{ID: uuid.New(), OrderIndex: 1, Text: "right", IsCorrect: true},
				{ID: uuid.New(), OrderIndex: 2, Text: "wrong A"},
				{ID: uuid.New(), OrderIndex: 3, Text: "wrong B"},
			},
		})
	}
	return qs
}

// pickFirstNCorrect answers the first n questions correctly and the rest
// wrong (second option). Pass wrongRest=false to leave the rest unanswered.
func pickFirstNCorrect(qs []Question, n int, wrongRest bool) map[uuid.UUID]uuid.UUID {
	picks := make(map[uuid.UUID]uuid.UUID, len(qs))
	for i, q := range qs {
		if i < n {
			picks[q.ID] = q.Options[0].ID
		} else if wrongRest {
			picks[q.ID] = q.Options[1].ID
		}
	}
	return picks
}

func TestGradeAttempt_ScoreAndThreshold(t *testing.T) {
	// The exam has 20 questions; PassingScore is the pass threshold the code
	// actually applies (score >= PassingScore).
	const total = 20
	require.LessOrEqual(t, PassingScore, total, "threshold must be reachable")

	tests := []struct {
		name       string
		correct    int  // questions answered correctly
		wrongRest  bool // answer the remainder wrong (true) or leave unanswered (false)
		wantScore  int
		wantPassed bool
	}{
		{name: "all correct", correct: total, wrongRest: false, wantScore: total, wantPassed: true},
		{name: "all wrong", correct: 0, wrongRest: true, wantScore: 0, wantPassed: false},
		{name: "partial, below threshold", correct: 10, wrongRest: true, wantScore: 10, wantPassed: false},
		{name: "exactly at threshold passes", correct: PassingScore, wrongRest: true, wantScore: PassingScore, wantPassed: true},
		{name: "one below threshold fails", correct: PassingScore - 1, wrongRest: true, wantScore: PassingScore - 1, wantPassed: false},
		{name: "empty picks grades everything wrong", correct: 0, wrongRest: false, wantScore: 0, wantPassed: false},
		{name: "unanswered remainder counts as wrong", correct: PassingScore - 1, wrongRest: false, wantScore: PassingScore - 1, wantPassed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qs := makeExam(total)
			picks := pickFirstNCorrect(qs, tt.correct, tt.wrongRest)

			got, err := gradeAttempt(qs, picks)
			require.NoError(t, err)

			assert.Equal(t, tt.wantScore, got.score)
			assert.Equal(t, total, got.total)
			assert.Equal(t, tt.wantPassed, got.passed)
			require.Len(t, got.answers, total)
		})
	}
}

func TestGradeAttempt_PerQuestionDetails(t *testing.T) {
	qs := makeExam(3)
	picks := map[uuid.UUID]uuid.UUID{
		qs[0].ID: qs[0].Options[0].ID, // correct
		qs[1].ID: qs[1].Options[2].ID, // wrong
		// qs[2] left unanswered
	}

	got, err := gradeAttempt(qs, picks)
	require.NoError(t, err)

	assert.Equal(t, 1, got.score)
	assert.Equal(t, 3, got.total)
	assert.False(t, got.passed)
	require.Len(t, got.answers, 3)

	// Answers are ordered like the questions slice.
	a0, a1, a2 := got.answers[0], got.answers[1], got.answers[2]

	assert.Equal(t, qs[0].ID, a0.questionID)
	require.NotNil(t, a0.selectedOptionID)
	assert.Equal(t, qs[0].Options[0].ID, *a0.selectedOptionID)
	assert.Equal(t, qs[0].Options[0].ID, a0.correctOptionID)
	assert.True(t, a0.isCorrect)

	assert.Equal(t, qs[1].ID, a1.questionID)
	require.NotNil(t, a1.selectedOptionID)
	assert.Equal(t, qs[1].Options[2].ID, *a1.selectedOptionID)
	assert.Equal(t, qs[1].Options[0].ID, a1.correctOptionID)
	assert.False(t, a1.isCorrect)

	assert.Equal(t, qs[2].ID, a2.questionID)
	assert.Nil(t, a2.selectedOptionID, "unanswered question keeps a nil selection")
	assert.Equal(t, qs[2].Options[0].ID, a2.correctOptionID)
	assert.False(t, a2.isCorrect)
}

func TestGradeAttempt_UnknownQuestionPickIsIgnored(t *testing.T) {
	// A pick keyed by a question id that isn't part of the exam is silently
	// ignored — the grading loop iterates the question list, not the picks map.
	qs := makeExam(2)
	picks := map[uuid.UUID]uuid.UUID{
		qs[0].ID:   qs[0].Options[0].ID,
		uuid.New(): uuid.New(), // nonexistent question
	}

	got, err := gradeAttempt(qs, picks)
	require.NoError(t, err)

	assert.Equal(t, 1, got.score)
	assert.Equal(t, 2, got.total)
	require.Len(t, got.answers, 2, "only real questions are graded")
}

func TestGradeAttempt_ForeignOptionIsRejected(t *testing.T) {
	tests := []struct {
		name string
		pick func(qs []Question) uuid.UUID
	}{
		{
			name: "option from another question",
			pick: func(qs []Question) uuid.UUID { return qs[1].Options[0].ID },
		},
		{
			name: "completely unknown option",
			pick: func(qs []Question) uuid.UUID { return uuid.New() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qs := makeExam(2)
			bad := tt.pick(qs)
			picks := map[uuid.UUID]uuid.UUID{qs[0].ID: bad}

			_, err := gradeAttempt(qs, picks)
			require.Error(t, err)
			// Sentinel, so the handler can answer 422 instead of 500; the ids
			// stay in the message for the logs.
			assert.ErrorIs(t, err, ErrPickNotOnQuestion)
			assert.EqualError(t, err,
				fmt.Sprintf("%s: option %s, question %s",
					ErrPickNotOnQuestion, bad, qs[0].ID))
		})
	}
}

// Duplicate picks per question are impossible by construction: picks is a
// map[questionID]optionID, so the last write wins before grading ever runs.
// What can recur is the same option id appearing on multiple questions; the
// belongs-check is per question, so each pick is validated against its own
// question's options only.
func TestGradeAttempt_SharedOptionIDAcrossQuestions(t *testing.T) {
	shared := uuid.New()
	qs := makeExam(2)
	qs[0].Options[1].ID = shared // wrong option on q0
	qs[1].Options[0].ID = shared // correct option on q1

	picks := map[uuid.UUID]uuid.UUID{
		qs[0].ID: shared,
		qs[1].ID: shared,
	}

	got, err := gradeAttempt(qs, picks)
	require.NoError(t, err)
	assert.Equal(t, 1, got.score, "shared id is wrong on q0 but correct on q1")
	assert.False(t, got.answers[0].isCorrect)
	assert.True(t, got.answers[1].isCorrect)
}

func TestGradeAttempt_QuestionWithNoCorrectOption(t *testing.T) {
	// Defensive: if seed data has no IsCorrect option, correctOptionID stays
	// uuid.Nil and every real pick grades wrong.
	qs := makeExam(1)
	qs[0].Options[0].IsCorrect = false

	picks := map[uuid.UUID]uuid.UUID{qs[0].ID: qs[0].Options[0].ID}
	got, err := gradeAttempt(qs, picks)
	require.NoError(t, err)

	assert.Equal(t, 0, got.score)
	assert.Equal(t, uuid.Nil, got.answers[0].correctOptionID)
	assert.False(t, got.answers[0].isCorrect)
}

func TestGradeAttempt_NoQuestions(t *testing.T) {
	// The store guards the empty exam before grading; the pure function itself
	// degrades to an empty, not-passed result (0 >= PassingScore is false).
	got, err := gradeAttempt(nil, map[uuid.UUID]uuid.UUID{uuid.New(): uuid.New()})
	require.NoError(t, err)
	assert.Equal(t, 0, got.score)
	assert.Equal(t, 0, got.total)
	assert.False(t, got.passed)
	assert.Empty(t, got.answers)
}

func TestQuestionReviews_ZipsQuestionsWithAnswers(t *testing.T) {
	qs := makeExam(2)
	picks := map[uuid.UUID]uuid.UUID{qs[0].ID: qs[0].Options[0].ID}

	got, err := gradeAttempt(qs, picks)
	require.NoError(t, err)

	reviews := questionReviews(qs, got.answers)
	require.Len(t, reviews, 2)

	assert.Equal(t, qs[0], reviews[0].Question, "full question (with options) is embedded")
	require.NotNil(t, reviews[0].SelectedOptionID)
	assert.Equal(t, qs[0].Options[0].ID, *reviews[0].SelectedOptionID)
	assert.Equal(t, qs[0].Options[0].ID, reviews[0].CorrectOptionID)
	assert.True(t, reviews[0].IsCorrect)

	assert.Equal(t, qs[1], reviews[1].Question)
	assert.Nil(t, reviews[1].SelectedOptionID)
	assert.Equal(t, qs[1].Options[0].ID, reviews[1].CorrectOptionID)
	assert.False(t, reviews[1].IsCorrect)
}

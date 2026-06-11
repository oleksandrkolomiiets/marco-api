package exam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubStore is a hand-written Store double. Each field configures one method's
// return values; SubmitAttempt records the picks map it was called with so
// tests can assert the handler's question_id → option_id translation.
type stubStore struct {
	questions    []Question
	questionsErr error

	submitReview *AttemptReview
	submitErr    error
	submitCalls  int
	gotUserID    uuid.UUID
	gotPicks     map[uuid.UUID]uuid.UUID

	latestReview *AttemptReview
	latestErr    error
}

func (s *stubStore) ListQuestions(_ context.Context) ([]Question, error) {
	return s.questions, s.questionsErr
}

func (s *stubStore) GetQuestionsForReview(_ context.Context) ([]Question, error) {
	return s.questions, s.questionsErr
}

func (s *stubStore) SubmitAttempt(_ context.Context, userID uuid.UUID, picks map[uuid.UUID]uuid.UUID) (*AttemptReview, error) {
	s.submitCalls++
	s.gotUserID = userID
	s.gotPicks = picks
	return s.submitReview, s.submitErr
}

func (s *stubStore) GetLatestAttempt(_ context.Context, _ uuid.UUID) (*AttemptReview, error) {
	return s.latestReview, s.latestErr
}

var _ Store = (*stubStore)(nil)

func newApp(h *Handler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	if userID != uuid.Nil {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user_id", userID)
			return c.Next()
		})
	}
	app.Get("/api/v1/exam/questions", h.ListQuestions)
	app.Post("/api/v1/exam/attempts", h.SubmitAttempt)
	app.Get("/api/v1/exam/attempts/latest", h.GetLatestAttempt)
	return app
}

func decodeError(t *testing.T, body io.Reader) string {
	t.Helper()
	var payload map[string]string
	require.NoError(t, json.NewDecoder(body).Decode(&payload))
	require.Contains(t, payload, "error")
	require.Len(t, payload, 1, "error shape must be exactly {\"error\":\"string\"}")
	return payload["error"]
}

func sampleQuestions() []Question {
	explanation := "because rules"
	return []Question{
		{
			ID:         uuid.New(),
			Slug:       "serve-height",
			OrderIndex: 1,
			Category:   "serving",
			Prompt:     "How high may you contact the ball on serve?",
			Options: []Option{
				{ID: uuid.New(), OrderIndex: 1, Text: "Waist height"},
				{ID: uuid.New(), OrderIndex: 2, Text: "Any height"},
			},
		},
		{
			ID:          uuid.New(),
			Slug:        "glass-play",
			OrderIndex:  2,
			Category:    "court",
			Prompt:      "Can the ball be played off the glass?",
			Explanation: &explanation,
			Options: []Option{
				{ID: uuid.New(), OrderIndex: 1, Text: "Yes"},
				{ID: uuid.New(), OrderIndex: 2, Text: "No"},
			},
		},
	}
}

// --- ListQuestions ---

func TestListQuestions_ReturnsQuestions(t *testing.T) {
	qs := sampleQuestions()
	store := &stubStore{questions: qs}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/exam/questions", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got []Question
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 2)
	assert.Equal(t, qs[0].ID, got[0].ID)
	assert.Equal(t, "serve-height", got[0].Slug)
	require.Len(t, got[0].Options, 2)
	assert.Equal(t, "Waist height", got[0].Options[0].Text)
	assert.False(t, got[0].Options[0].IsCorrect, "take-the-exam payload must never reveal correct options")
	assert.Equal(t, "glass-play", got[1].Slug)
}

func TestListQuestions_Unauthorized(t *testing.T) {
	app := newApp(NewHandler(&stubStore{}), uuid.Nil)

	req := httptest.NewRequest("GET", "/api/v1/exam/questions", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "unauthorized", decodeError(t, resp.Body))
}

func TestListQuestions_StoreError(t *testing.T) {
	store := &stubStore{questionsErr: errors.New("db down")}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/exam/questions", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal server error", decodeError(t, resp.Body))
}

// --- SubmitAttempt ---

func TestSubmitAttempt_Valid(t *testing.T) {
	userID := uuid.New()
	q1 := uuid.New()
	q2 := uuid.New()
	o1 := uuid.New()

	review := &AttemptReview{
		Attempt: Attempt{
			ID:          uuid.New(),
			UserID:      userID,
			Score:       19,
			Total:       20,
			Passed:      true,
			CompletedAt: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
		},
		Questions: []QuestionReview{},
	}
	store := &stubStore{submitReview: review}
	app := newApp(NewHandler(store), userID)

	// q2 is answered with a null option — it must be skipped, not parsed.
	body := `{"answers":[
		{"question_id":"` + q1.String() + `","selected_option_id":"` + o1.String() + `"},
		{"question_id":"` + q2.String() + `","selected_option_id":null}
	]}`
	req := httptest.NewRequest("POST", "/api/v1/exam/attempts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var got AttemptReview
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, review.ID, got.ID)
	assert.Equal(t, 19, got.Score)
	assert.Equal(t, 20, got.Total)
	assert.True(t, got.Passed)

	require.Equal(t, 1, store.submitCalls)
	assert.Equal(t, userID, store.gotUserID)
	assert.Equal(t, map[uuid.UUID]uuid.UUID{q1: o1}, store.gotPicks,
		"answered question keeps its pick; null pick is omitted from the map")
}

func TestSubmitAttempt_InvalidPayloads(t *testing.T) {
	validQ := uuid.New().String()
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "malformed json",
			body:       `{"answers": [`,
			wantStatus: fiber.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "non-json body",
			body:       `not json at all`,
			wantStatus: fiber.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "invalid question_id",
			body:       `{"answers":[{"question_id":"not-a-uuid","selected_option_id":null}]}`,
			wantStatus: fiber.StatusUnprocessableEntity,
			wantError:  "invalid question_id",
		},
		{
			name:       "empty question_id",
			body:       `{"answers":[{"question_id":"","selected_option_id":null}]}`,
			wantStatus: fiber.StatusUnprocessableEntity,
			wantError:  "invalid question_id",
		},
		{
			name:       "invalid selected_option_id",
			body:       `{"answers":[{"question_id":"` + validQ + `","selected_option_id":"nope"}]}`,
			wantStatus: fiber.StatusUnprocessableEntity,
			wantError:  "invalid selected_option_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{submitReview: &AttemptReview{}}
			app := newApp(NewHandler(store), uuid.New())

			req := httptest.NewRequest("POST", "/api/v1/exam/attempts", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			assert.Equal(t, tt.wantError, decodeError(t, resp.Body))
			assert.Zero(t, store.submitCalls, "store must not be reached on invalid input")
		})
	}
}

func TestSubmitAttempt_EmptyAnswersStillSubmits(t *testing.T) {
	// An empty answer list is valid input: the store grades every question as
	// unanswered/wrong. The handler just forwards an empty picks map.
	store := &stubStore{submitReview: &AttemptReview{Attempt: Attempt{Score: 0, Total: 20}}}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/exam/attempts", strings.NewReader(`{"answers":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
	require.Equal(t, 1, store.submitCalls)
	assert.Empty(t, store.gotPicks)
}

func TestSubmitAttempt_Unauthorized(t *testing.T) {
	store := &stubStore{}
	app := newApp(NewHandler(store), uuid.Nil)

	req := httptest.NewRequest("POST", "/api/v1/exam/attempts", strings.NewReader(`{"answers":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "unauthorized", decodeError(t, resp.Body))
	assert.Zero(t, store.submitCalls)
}

func TestSubmitAttempt_StoreError(t *testing.T) {
	store := &stubStore{submitErr: errors.New("option mismatch")}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/exam/attempts", strings.NewReader(`{"answers":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal server error", decodeError(t, resp.Body))
}

// --- GetLatestAttempt ---

func TestGetLatestAttempt_Found(t *testing.T) {
	userID := uuid.New()
	selected := uuid.New()
	correct := uuid.New()
	review := &AttemptReview{
		Attempt: Attempt{
			ID:          uuid.New(),
			UserID:      userID,
			Score:       18,
			Total:       20,
			Passed:      true,
			CompletedAt: time.Date(2026, 6, 9, 9, 30, 0, 0, time.UTC),
		},
		Questions: []QuestionReview{
			{
				Question:         sampleQuestions()[0],
				SelectedOptionID: &selected,
				CorrectOptionID:  correct,
				IsCorrect:        false,
			},
		},
	}
	store := &stubStore{latestReview: review}
	app := newApp(NewHandler(store), userID)

	req := httptest.NewRequest("GET", "/api/v1/exam/attempts/latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got AttemptReview
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, review.ID, got.ID)
	assert.Equal(t, 18, got.Score)
	assert.True(t, got.Passed)
	require.Len(t, got.Questions, 1)
	require.NotNil(t, got.Questions[0].SelectedOptionID)
	assert.Equal(t, selected, *got.Questions[0].SelectedOptionID)
	assert.Equal(t, correct, got.Questions[0].CorrectOptionID)
	assert.False(t, got.Questions[0].IsCorrect)
}

func TestGetLatestAttempt_NoneYet(t *testing.T) {
	store := &stubStore{latestErr: pgx.ErrNoRows}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/exam/attempts/latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "no attempt yet", decodeError(t, resp.Body))
}

func TestGetLatestAttempt_WrappedNoRowsIsStill404(t *testing.T) {
	store := &stubStore{latestErr: errors.Join(errors.New("get latest"), pgx.ErrNoRows)}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/exam/attempts/latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestGetLatestAttempt_StoreError(t *testing.T) {
	store := &stubStore{latestErr: errors.New("db down")}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/exam/attempts/latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal server error", decodeError(t, resp.Body))
}

func TestGetLatestAttempt_Unauthorized(t *testing.T) {
	app := newApp(NewHandler(&stubStore{}), uuid.Nil)

	req := httptest.NewRequest("GET", "/api/v1/exam/attempts/latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "unauthorized", decodeError(t, resp.Body))
}

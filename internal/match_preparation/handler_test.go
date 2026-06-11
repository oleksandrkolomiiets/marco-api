package match_preparation

import (
	"bytes"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"marco-api/internal/anthropic"
	"marco-api/internal/marco"
)

type stubStore struct {
	created       *CreateParams
	listResult    []Preparation
	getResult     Preparation
	getErr        error
	updateParams  *UpdateParams
	replaceInput  []DrillInput
	toggledDrill  uuid.UUID
	toggledFlag   bool
	deletedID     uuid.UUID
	createReturns Preparation
}

func (s *stubStore) Create(_ context.Context, p CreateParams) (Preparation, error) {
	s.created = &p
	return s.createReturns, nil
}

func (s *stubStore) Get(_ context.Context, _, _ uuid.UUID) (Preparation, error) {
	if s.getErr != nil {
		return Preparation{}, s.getErr
	}
	return s.getResult, nil
}

func (s *stubStore) List(_ context.Context, _ uuid.UUID) ([]Preparation, error) {
	return s.listResult, nil
}

func (s *stubStore) Update(_ context.Context, p UpdateParams) (Preparation, error) {
	s.updateParams = &p
	return s.getResult, nil
}

func (s *stubStore) ReplaceDrills(_ context.Context, _, _ uuid.UUID, inputs []DrillInput) (Preparation, error) {
	s.replaceInput = inputs
	return s.getResult, nil
}

func (s *stubStore) SetDrillCompleted(_ context.Context, _, drillID uuid.UUID, completed bool) (Drill, error) {
	s.toggledDrill = drillID
	s.toggledFlag = completed
	return Drill{ID: drillID, Completed: completed}, nil
}

func (s *stubStore) Delete(_ context.Context, _, id uuid.UUID) error {
	s.deletedID = id
	return nil
}

var _ storeIface = (*stubStore)(nil)

type stubAssembler struct {
	uc  marco.UserContext
	err error
}

func (s *stubAssembler) Build(_ context.Context, _ uuid.UUID) (marco.UserContext, []anthropic.Message, error) {
	return s.uc, nil, s.err
}

var _ assemblerIface = (*stubAssembler)(nil)

func newTestApp(h *Handler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/api/v1/match-preparation", h.Create)
	app.Get("/api/v1/match-preparation", h.List)
	app.Get("/api/v1/match-preparation/:id", h.Get)
	app.Patch("/api/v1/match-preparation/:id", h.Update)
	app.Delete("/api/v1/match-preparation/:id", h.Delete)
	app.Put("/api/v1/match-preparation/:id/drills", h.ReplaceDrills)
	app.Patch("/api/v1/match-preparation/:id/drills/:drillId", h.ToggleDrill)
	app.Post("/api/v1/match-preparation/:id/suggest-drills", h.SuggestDrills)
	return app
}

func TestHandler_Create_TrimsAndPersists(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	body := bytes.NewBufferString(`{
		"scheduled_at": "2026-05-20T20:00:00Z",
		"opponents": ["  Lucia  ", "Pablo", ""],
		"partner_name": "  Julia ",
		"court": "CT 3",
		"note": "  Lost 5-7 last time. Bandeja was the gap.  ",
		"drills": [
			{"title": "  Bandeja — paddle path  ", "duration_seconds": 90},
			{"title": "Defensive stance reset", "duration_seconds": 300}
		]
	}`)
	req := httptest.NewRequest("POST", "/api/v1/match-preparation", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	require.NotNil(t, store.created)
	assert.Equal(t, []string{"Lucia", "Pablo"}, store.created.Opponents)
	require.NotNil(t, store.created.PartnerName)
	assert.Equal(t, "Julia", *store.created.PartnerName)
	require.NotNil(t, store.created.Note)
	assert.Equal(t, "Lost 5-7 last time. Bandeja was the gap.", *store.created.Note)
	require.Len(t, store.created.Drills, 2)
	assert.Equal(t, "Bandeja — paddle path", store.created.Drills[0].Title)
	assert.Equal(t, 90, store.created.Drills[0].DurationSeconds)
}

func TestHandler_Create_RejectsBadScheduledAt(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/match-preparation", bytes.NewBufferString(`{"scheduled_at": "tomorrow"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.Nil(t, store.created)
}

func TestHandler_Create_RejectsTooManyOpponents(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/match-preparation", bytes.NewBufferString(`{
		"scheduled_at": "2026-05-20T20:00:00Z",
		"opponents": ["a","b","c","d"]
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestHandler_Update_ParsesPlanGradeAndClear(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	id := uuid.New()
	for _, grade := range []string{"worked", "mixed", "missed"} {
		store.updateParams = nil
		req := httptest.NewRequest("PATCH", "/api/v1/match-preparation/"+id.String(), bytes.NewBufferString(`{"plan_grade":"`+grade+`"}`))
		req.Header.Set("Content-Type", "application/json")
		_, err := app.Test(req)
		require.NoError(t, err)
		require.NotNil(t, store.updateParams)
		require.NotNil(t, store.updateParams.PlanGrade, "grade=%s", grade)
		assert.Equal(t, grade, *store.updateParams.PlanGrade)
		assert.False(t, store.updateParams.ClearGrade)
	}

	// Reset and post empty string to clear.
	store.updateParams = nil
	req := httptest.NewRequest("PATCH", "/api/v1/match-preparation/"+id.String(), bytes.NewBufferString(`{"plan_grade":""}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := app.Test(req)
	require.NoError(t, err)
	require.NotNil(t, store.updateParams)
	assert.True(t, store.updateParams.ClearGrade)
	assert.Nil(t, store.updateParams.PlanGrade)
}

func TestHandler_Update_RejectsInvalidGrade(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	id := uuid.New()
	req := httptest.NewRequest("PATCH", "/api/v1/match-preparation/"+id.String(), bytes.NewBufferString(`{"plan_grade":"meh"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestHandler_Update_LinksMatchLog(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	id := uuid.New()
	matchID := uuid.New()
	req := httptest.NewRequest("PATCH", "/api/v1/match-preparation/"+id.String(), bytes.NewBufferString(`{"match_log_id":"`+matchID.String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := app.Test(req)
	require.NoError(t, err)
	require.NotNil(t, store.updateParams)
	require.NotNil(t, store.updateParams.MatchLogID)
	assert.Equal(t, matchID, *store.updateParams.MatchLogID)
}

func TestHandler_ToggleDrill(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	parent := uuid.New()
	drill := uuid.New()
	req := httptest.NewRequest("PATCH", "/api/v1/match-preparation/"+parent.String()+"/drills/"+drill.String(), bytes.NewBufferString(`{"completed":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, drill, store.toggledDrill)
	assert.True(t, store.toggledFlag)
}

func TestHandler_ReplaceDrills(t *testing.T) {
	store := &stubStore{}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	id := uuid.New()
	req := httptest.NewRequest("PUT", "/api/v1/match-preparation/"+id.String()+"/drills", bytes.NewBufferString(`{
		"drills": [
			{"title": "Bandeja — paddle path", "duration_seconds": 90},
			{"title": "Vibora — pace control", "duration_seconds": 360}
		]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Len(t, store.replaceInput, 2)
	assert.Equal(t, "Vibora — pace control", store.replaceInput[1].Title)
}

func TestHandler_Get_NotFound(t *testing.T) {
	store := &stubStore{getErr: ErrNotFound}
	h := NewHandler(store, &anthropic.MockClient{}, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	id := uuid.New()
	req := httptest.NewRequest("GET", "/api/v1/match-preparation/"+id.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestHandler_SuggestDrills_ParsesJSONArray(t *testing.T) {
	store := &stubStore{
		getResult: Preparation{
			ID:          uuid.New(),
			ScheduledAt: time.Now(),
			Opponents:   []string{"Lucia", "Pablo"},
		},
	}
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{IsDone: true, FinalText: `[
			{"title": "Lob recovery shadow", "duration_seconds": 480},
			{"title": "Serve placement reps", "duration_seconds": 600}
		]`},
	}, nil)

	h := NewHandler(store, mock, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/match-preparation/"+store.getResult.ID.String()+"/suggest-drills", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var out []suggestion
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out, 2)
	assert.Equal(t, "Lob recovery shadow", out[0].Title)
	assert.Equal(t, 480, out[0].DurationSeconds)
}

func TestHandler_SuggestDrills_HandlesProseAroundJSON(t *testing.T) {
	store := &stubStore{getResult: Preparation{ID: uuid.New()}}
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{IsDone: true, FinalText: "Sure! Here are some ideas:\n[{\"title\":\"Backhand return\",\"duration_seconds\":360}]\nLet me know."},
	}, nil)

	h := NewHandler(store, mock, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/match-preparation/"+store.getResult.ID.String()+"/suggest-drills", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, strings.Contains(string(body), "Backhand return"))
}

func TestHandler_SuggestDrills_PropagatesStreamError(t *testing.T) {
	store := &stubStore{getResult: Preparation{ID: uuid.New()}}
	mock := &anthropic.MockClient{}
	mock.Setup(nil, errors.New("boom"))

	h := NewHandler(store, mock, &stubAssembler{})
	app := newTestApp(h, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/match-preparation/"+store.getResult.ID.String()+"/suggest-drills", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadGateway, resp.StatusCode)
}

func TestParseSuggestions_ClampsDuration(t *testing.T) {
	out, err := parseSuggestions(`[
		{"title": "way too long", "duration_seconds": 9999},
		{"title": "way too short", "duration_seconds": 10},
		{"title": "  ", "duration_seconds": 600}
	]`)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, 1800, out[0].DurationSeconds)
	assert.Equal(t, 60, out[1].DurationSeconds)
}

func TestParseSuggestions_EmptyResponse(t *testing.T) {
	_, err := parseSuggestions("")
	assert.Error(t, err)
}

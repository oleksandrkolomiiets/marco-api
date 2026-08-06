package logs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMatchStore struct {
	match    MatchLog
	matches  []MatchLog
	partners []PartnerSuggestion

	createErr   error
	updateErr   error
	listErr     error
	partnersErr error

	gotCreate *CreateMatchParams
	gotUpdate *UpdateMatchParams
}

func (s *stubMatchStore) CreateMatch(_ context.Context, p CreateMatchParams) (MatchLog, error) {
	s.gotCreate = &p
	if s.createErr != nil {
		return MatchLog{}, s.createErr
	}
	return s.match, nil
}

func (s *stubMatchStore) UpdateMatch(_ context.Context, p UpdateMatchParams) (MatchLog, error) {
	s.gotUpdate = &p
	if s.updateErr != nil {
		return MatchLog{}, s.updateErr
	}
	return s.match, nil
}

func (s *stubMatchStore) ListMatches(_ context.Context, _ uuid.UUID) ([]MatchLog, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.matches, nil
}

func (s *stubMatchStore) ListPartners(_ context.Context, _ uuid.UUID) ([]PartnerSuggestion, error) {
	if s.partnersErr != nil {
		return nil, s.partnersErr
	}
	return s.partners, nil
}

var _ matchStoreIface = (*stubMatchStore)(nil)

func registerLogsRoutes(app *fiber.App, h *LogsHandler) {
	app.Post("/api/v1/logs/matches", h.CreateMatch)
	app.Put("/api/v1/logs/matches/:id", h.UpdateMatch)
	app.Get("/api/v1/logs/matches", h.ListMatches)
	app.Get("/api/v1/logs/partners", h.ListPartners)
}

func newLogsApp(h *LogsHandler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	registerLogsRoutes(app, h)
	return app
}

func newLogsAppNoAuth(h *LogsHandler) *fiber.App {
	app := fiber.New()
	registerLogsRoutes(app, h)
	return app
}

func strPtr(s string) *string { return &s }

func testMatch(userID uuid.UUID) MatchLog {
	return MatchLog{
		ID:          uuid.New(),
		UserID:      userID,
		Played:      true,
		Result:      strPtr("won"),
		Feeling:     strPtr("great"),
		Note:        strPtr("solid bandejas"),
		PartnerName: strPtr("Ana"),
		Opponents:   []string{"Marco", "Luis"},
		PlayedOn:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestLogsHandler_CreateMatch_Success(t *testing.T) {
	userID := uuid.New()
	store := &stubMatchStore{match: testMatch(userID)}
	app := newLogsApp(NewHandler(store), userID)

	messageID := uuid.New()
	reqBody := `{
		"played_on": "2026-06-01",
		"result": "won",
		"feeling": "great",
		"note": "solid bandejas",
		"partner_name": "  Ana  ",
		"opponents": ["  Marco ", "", "  ", "Luis"],
		"message_id": "` + messageID.String() + `"
	}`
	req := httptest.NewRequest("POST", "/api/v1/logs/matches", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var got MatchLog
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, store.match.ID, got.ID)
	assert.Equal(t, userID, got.UserID)
	require.NotNil(t, got.Result)
	assert.Equal(t, "won", *got.Result)
	assert.Equal(t, []string{"Marco", "Luis"}, got.Opponents)

	// The handler must normalize input before hitting the store.
	require.NotNil(t, store.gotCreate)
	assert.Equal(t, userID, store.gotCreate.UserID)
	assert.Equal(t, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), store.gotCreate.PlayedOn)
	require.NotNil(t, store.gotCreate.PartnerName)
	assert.Equal(t, "Ana", *store.gotCreate.PartnerName, "partner_name must be trimmed")
	assert.Equal(t, []string{"Marco", "Luis"}, store.gotCreate.Opponents, "opponents must be trimmed and empties dropped")
	require.NotNil(t, store.gotCreate.MessageID)
	assert.Equal(t, messageID, *store.gotCreate.MessageID)
}

func TestLogsHandler_CreateMatch_BlankPartnerBecomesNil(t *testing.T) {
	userID := uuid.New()
	store := &stubMatchStore{match: testMatch(userID)}
	app := newLogsApp(NewHandler(store), userID)

	reqBody := `{"played_on":"2026-06-01","result":"won","partner_name":"   "}`
	req := httptest.NewRequest("POST", "/api/v1/logs/matches", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)
	require.NotNil(t, store.gotCreate)
	assert.Nil(t, store.gotCreate.PartnerName, "whitespace-only partner_name must be stored as nil")
	assert.Nil(t, store.gotCreate.MessageID, "absent message_id must stay nil")
}

func TestLogsHandler_CreateMatch_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			"missing played_on",
			`{"result":"won"}`,
			"played_on is required",
		},
		{
			"whitespace played_on",
			`{"played_on":"   "}`,
			"played_on is required",
		},
		{
			"bad played_on format",
			`{"played_on":"01-06-2026"}`,
			"played_on must be YYYY-MM-DD",
		},
		{
			// A row with no result reads as "Played", filters into neither
			// Wins nor Losses, and counts toward the total but not the record.
			"missing result",
			`{"played_on":"2026-06-01"}`,
			"result is required",
		},
		{
			"null result",
			`{"played_on":"2026-06-01","result":null}`,
			"result is required",
		},
		{
			"invalid result",
			`{"played_on":"2026-06-01","result":"crushed"}`,
			"result must be won, lost, or draw",
		},
		{
			"feeling too long",
			`{"played_on":"2026-06-01","result":"won","feeling":"` + strings.Repeat("f", 51) + `"}`,
			"feeling must be 50 characters or fewer",
		},
		{
			"note too long",
			`{"played_on":"2026-06-01","result":"won","note":"` + strings.Repeat("n", 2001) + `"}`,
			"note must be 2000 characters or fewer",
		},
		{
			"opponent name too long",
			`{"played_on":"2026-06-01","result":"won","opponents":["` + strings.Repeat("o", 101) + `"]}`,
			"opponent name must be 100 characters or fewer",
		},
		{
			"too many opponents",
			`{"played_on":"2026-06-01","result":"won","opponents":["a","b","c","d"]}`,
			"at most 3 opponents allowed",
		},
		{
			"invalid message_id",
			`{"played_on":"2026-06-01","result":"won","message_id":"not-a-uuid"}`,
			"invalid message_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubMatchStore{}
			app := newLogsApp(NewHandler(store), uuid.New())

			req := httptest.NewRequest("POST", "/api/v1/logs/matches", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
			var body map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, tt.wantErr, body["error"])
			assert.Nil(t, store.gotCreate, "store must not be called on validation failure")
		})
	}
}

func TestLogsHandler_CreateMatch_InvalidBody(t *testing.T) {
	store := &stubMatchStore{}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/logs/matches", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid request body", body["error"])
}

func TestLogsHandler_CreateMatch_StoreError(t *testing.T) {
	store := &stubMatchStore{createErr: errors.New("db down")}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/logs/matches", strings.NewReader(`{"played_on":"2026-06-01","result":"won"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "failed to create match log", body["error"])
}

func TestLogsHandler_Unauthorized(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"CreateMatch", "POST", "/api/v1/logs/matches", `{"played_on":"2026-06-01","result":"won"}`},
		{"UpdateMatch", "PUT", "/api/v1/logs/matches/" + uuid.New().String(), `{"played_on":"2026-06-01","result":"won"}`},
		{"ListMatches", "GET", "/api/v1/logs/matches", ""},
		{"ListPartners", "GET", "/api/v1/logs/partners", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newLogsAppNoAuth(NewHandler(&stubMatchStore{}))

			var reader *strings.Reader
			if tt.body != "" {
				reader = strings.NewReader(tt.body)
			} else {
				reader = strings.NewReader("")
			}
			req := httptest.NewRequest(tt.method, tt.path, reader)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
			var body map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, "unauthorized", body["error"])
		})
	}
}

func TestLogsHandler_UpdateMatch_Success(t *testing.T) {
	userID := uuid.New()
	updated := testMatch(userID)
	updated.Result = strPtr("lost")
	store := &stubMatchStore{match: updated}
	app := newLogsApp(NewHandler(store), userID)

	matchID := updated.ID
	reqBody := `{"played_on":"2026-06-02","result":"lost","opponents":[" Pablo "]}`
	req := httptest.NewRequest("PUT", "/api/v1/logs/matches/"+matchID.String(), strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got MatchLog
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, matchID, got.ID)
	require.NotNil(t, got.Result)
	assert.Equal(t, "lost", *got.Result)

	require.NotNil(t, store.gotUpdate)
	assert.Equal(t, userID, store.gotUpdate.UserID)
	assert.Equal(t, matchID, store.gotUpdate.MatchID)
	assert.Equal(t, time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), store.gotUpdate.PlayedOn)
	assert.Equal(t, []string{"Pablo"}, store.gotUpdate.Opponents)
}

func TestLogsHandler_UpdateMatch_InvalidMatchID(t *testing.T) {
	store := &stubMatchStore{}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PUT", "/api/v1/logs/matches/not-a-uuid", strings.NewReader(`{"played_on":"2026-06-01","result":"won"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "invalid match id", body["error"])
	assert.Nil(t, store.gotUpdate)
}

func TestLogsHandler_UpdateMatch_ValidationError(t *testing.T) {
	// UpdateMatch shares validateMatchInput with CreateMatch — one case proves
	// the wiring, the matrix is covered in the CreateMatch table test.
	store := &stubMatchStore{}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PUT", "/api/v1/logs/matches/"+uuid.New().String(),
		strings.NewReader(`{"played_on":"2026-06-01","result":"smashed"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "result must be won, lost, or draw", body["error"])
	assert.Nil(t, store.gotUpdate)
}

func TestLogsHandler_UpdateMatch_NotFound(t *testing.T) {
	store := &stubMatchStore{updateErr: ErrMatchNotFound}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PUT", "/api/v1/logs/matches/"+uuid.New().String(),
		strings.NewReader(`{"played_on":"2026-06-01","result":"won"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "match not found", body["error"])
}

func TestLogsHandler_UpdateMatch_StoreError(t *testing.T) {
	store := &stubMatchStore{updateErr: errors.New("db down")}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PUT", "/api/v1/logs/matches/"+uuid.New().String(),
		strings.NewReader(`{"played_on":"2026-06-01","result":"won"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "failed to update match log", body["error"])
}

func TestLogsHandler_ListMatches_Success(t *testing.T) {
	userID := uuid.New()
	store := &stubMatchStore{matches: []MatchLog{testMatch(userID), testMatch(userID)}}
	app := newLogsApp(NewHandler(store), userID)

	req := httptest.NewRequest("GET", "/api/v1/logs/matches", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got []MatchLog
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 2)
	assert.Equal(t, store.matches[0].ID, got[0].ID)
	assert.Equal(t, userID, got[0].UserID)
}

func TestLogsHandler_ListMatches_StoreError(t *testing.T) {
	store := &stubMatchStore{listErr: errors.New("db down")}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/logs/matches", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "failed to list matches", body["error"])
}

func TestLogsHandler_ListPartners_Success(t *testing.T) {
	store := &stubMatchStore{partners: []PartnerSuggestion{
		{PartnerName: "Ana", MatchCount: 5},
		{PartnerName: "Luis", MatchCount: 2},
	}}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/logs/partners", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got []PartnerSuggestion
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got, 2)
	assert.Equal(t, "Ana", got[0].PartnerName)
	assert.Equal(t, 5, got[0].MatchCount)
	assert.Equal(t, "Luis", got[1].PartnerName)
}

func TestLogsHandler_ListPartners_StoreError(t *testing.T) {
	store := &stubMatchStore{partnersErr: errors.New("db down")}
	app := newLogsApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/logs/partners", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "failed to list partners", body["error"])
}

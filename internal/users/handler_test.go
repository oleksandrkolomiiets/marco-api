package users

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

type stubUserStore struct {
	user *User
	err  error

	gotGetID        uuid.UUID
	gotUpdateID     uuid.UUID
	gotUpdateParams UpdateUserParams
}

func (s *stubUserStore) CreateUser(_ context.Context, _ CreateUserParams) (*User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByGoogleID(_ context.Context, _ string) (*User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByEmail(_ context.Context, _ string) (*User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByID(_ context.Context, id uuid.UUID) (*User, error) {
	s.gotGetID = id
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func (s *stubUserStore) UpdateUser(_ context.Context, id uuid.UUID, params UpdateUserParams) (*User, error) {
	s.gotUpdateID = id
	s.gotUpdateParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

var _ UserStore = (*stubUserStore)(nil)

func newUsersApp(handler *Handler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Get("/api/v1/users/me", handler.GetMe)
	app.Patch("/api/v1/users/me", handler.UpdateMe)
	return app
}

func newUsersAppNoAuth(handler *Handler) *fiber.App {
	app := fiber.New()
	app.Get("/api/v1/users/me", handler.GetMe)
	app.Patch("/api/v1/users/me", handler.UpdateMe)
	return app
}

func strPtr(s string) *string { return &s }

func testUser(id uuid.UUID) *User {
	return &User{
		ID:            id,
		Email:         "joost@example.com",
		DisplayName:   strPtr("Joost"),
		SkillLevel:    strPtr("intermediate"),
		DominantHand:  strPtr("right"),
		CourtSide:     strPtr("left"),
		PlayFrequency: strPtr("weekly"),
		Goal:          strPtr("improve bandeja"),
		Plan:          "free",
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func decodeJSONBody(t *testing.T, resp io.Reader) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp).Decode(&out))
	return out
}

func TestHandler_GetMe_ReturnsUser(t *testing.T) {
	userID := uuid.New()
	store := &stubUserStore{user: testUser(userID)}
	app := newUsersApp(NewHandler(store), userID)

	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, userID.String(), body["id"])
	assert.Equal(t, "joost@example.com", body["email"])
	assert.Equal(t, "Joost", body["display_name"])
	assert.Equal(t, "intermediate", body["skill_level"])
	assert.Equal(t, "free", body["plan"])
	// GoogleID and PasswordHash are json:"-" — they must never leak.
	assert.NotContains(t, body, "GoogleID")
	assert.NotContains(t, body, "PasswordHash")
	assert.NotContains(t, body, "google_id")
	assert.NotContains(t, body, "password_hash")

	assert.Equal(t, userID, store.gotGetID)
}

func TestHandler_GetMe_NotFound(t *testing.T) {
	store := &stubUserStore{err: pgx.ErrNoRows}
	app := newUsersApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "user not found", body["error"])
}

func TestHandler_GetMe_StoreError(t *testing.T) {
	store := &stubUserStore{err: errors.New("db down")}
	app := newUsersApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "internal server error", body["error"])
}

func TestHandler_Unauthorized(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   io.Reader
	}{
		{"GetMe without user_id", "GET", nil},
		{"UpdateMe without user_id", "PATCH", strings.NewReader(`{"goal":"win"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubUserStore{user: testUser(uuid.New())}
			app := newUsersAppNoAuth(NewHandler(store))

			req := httptest.NewRequest(tt.method, "/api/v1/users/me", tt.body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
			body := decodeJSONBody(t, resp.Body)
			assert.Equal(t, "unauthorized", body["error"])
		})
	}
}

func TestHandler_UpdateMe_Success(t *testing.T) {
	userID := uuid.New()
	updated := testUser(userID)
	updated.SkillLevel = strPtr("advanced")
	updated.Goal = strPtr("win the club tournament")
	store := &stubUserStore{user: updated}
	app := newUsersApp(NewHandler(store), userID)

	reqBody := `{"skill_level":"advanced","goal":"win the club tournament"}`
	req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "advanced", body["skill_level"])
	assert.Equal(t, "win the club tournament", body["goal"])

	assert.Equal(t, userID, store.gotUpdateID)
	require.NotNil(t, store.gotUpdateParams.SkillLevel)
	assert.Equal(t, "advanced", *store.gotUpdateParams.SkillLevel)
	require.NotNil(t, store.gotUpdateParams.Goal)
	assert.Equal(t, "win the club tournament", *store.gotUpdateParams.Goal)
	assert.Nil(t, store.gotUpdateParams.DominantHand)
	assert.Nil(t, store.gotUpdateParams.CourtSide)
	assert.Nil(t, store.gotUpdateParams.PlayFrequency)
}

func TestHandler_UpdateMe_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			"invalid skill_level",
			`{"skill_level":"pro"}`,
			"skill_level must be one of: beginner, intermediate, advanced",
		},
		{
			"invalid dominant_hand",
			`{"dominant_hand":"ambidextrous"}`,
			"dominant_hand must be one of: left, right, both",
		},
		{
			"invalid court_side",
			`{"court_side":"middle"}`,
			"court_side must be one of: left, right, either",
		},
		{
			"play_frequency too long",
			`{"play_frequency":"` + strings.Repeat("a", 21) + `"}`,
			"play_frequency must be 20 characters or fewer",
		},
		{
			"goal too long",
			`{"goal":"` + strings.Repeat("g", 51) + `"}`,
			"goal must be 50 characters or fewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubUserStore{user: testUser(uuid.New())}
			app := newUsersApp(NewHandler(store), uuid.New())

			req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnprocessableEntity, resp.StatusCode)
			body := decodeJSONBody(t, resp.Body)
			assert.Equal(t, tt.wantErr, body["error"])
		})
	}
}

func TestHandler_UpdateMe_AcceptsBoundaryLengths(t *testing.T) {
	// Length caps are rune counts: exactly 20 for play_frequency and 50 for goal pass.
	store := &stubUserStore{user: testUser(uuid.New())}
	app := newUsersApp(NewHandler(store), uuid.New())

	reqBody := `{"play_frequency":"` + strings.Repeat("é", 20) + `","goal":"` + strings.Repeat("é", 50) + `"}`
	req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.NotNil(t, store.gotUpdateParams.PlayFrequency)
	assert.Equal(t, strings.Repeat("é", 20), *store.gotUpdateParams.PlayFrequency)
}

func TestHandler_UpdateMe_InvalidBody(t *testing.T) {
	store := &stubUserStore{user: testUser(uuid.New())}
	app := newUsersApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "invalid request body", body["error"])
}

func TestHandler_UpdateMe_NotFound(t *testing.T) {
	store := &stubUserStore{err: pgx.ErrNoRows}
	app := newUsersApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(`{"goal":"win"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "user not found", body["error"])
}

func TestHandler_UpdateMe_StoreError(t *testing.T) {
	store := &stubUserStore{err: errors.New("db down")}
	app := newUsersApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("PATCH", "/api/v1/users/me", strings.NewReader(`{"goal":"win"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	body := decodeJSONBody(t, resp.Body)
	assert.Equal(t, "internal server error", body["error"])
}

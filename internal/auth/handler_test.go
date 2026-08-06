package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marco-api/internal/config"
	"marco-api/internal/email"
	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
)

// stubUserStore implements users.UserStore. Unset funcs return pgx.ErrNoRows
// so each test only wires the behaviour it cares about.
type stubUserStore struct {
	createFn   func(users.CreateUserParams) (*users.User, error)
	byEmailFn  func(string) (*users.User, error)
	byGoogleFn func(string) (*users.User, error)
	byIDFn     func(uuid.UUID) (*users.User, error)
	updateFn   func(uuid.UUID, users.UpdateUserParams) (*users.User, error)

	createdParams []users.CreateUserParams
	byEmailArgs   []string
	updateCalls   int

	passwordUpdates   []passwordUpdate
	updatePasswordErr error
}

func (s *stubUserStore) CreateUser(_ context.Context, p users.CreateUserParams) (*users.User, error) {
	s.createdParams = append(s.createdParams, p)
	if s.createFn == nil {
		return nil, pgx.ErrNoRows
	}
	return s.createFn(p)
}

func (s *stubUserStore) GetUserByEmail(_ context.Context, email string) (*users.User, error) {
	s.byEmailArgs = append(s.byEmailArgs, email)
	if s.byEmailFn == nil {
		return nil, pgx.ErrNoRows
	}
	return s.byEmailFn(email)
}

func (s *stubUserStore) GetUserByGoogleID(_ context.Context, googleID string) (*users.User, error) {
	if s.byGoogleFn == nil {
		return nil, pgx.ErrNoRows
	}
	return s.byGoogleFn(googleID)
}

func (s *stubUserStore) GetUserByID(_ context.Context, id uuid.UUID) (*users.User, error) {
	if s.byIDFn == nil {
		return nil, pgx.ErrNoRows
	}
	return s.byIDFn(id)
}

func (s *stubUserStore) UpdateUser(_ context.Context, id uuid.UUID, p users.UpdateUserParams) (*users.User, error) {
	s.updateCalls++
	if s.updateFn == nil {
		return nil, pgx.ErrNoRows
	}
	return s.updateFn(id, p)
}

func (s *stubUserStore) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	s.passwordUpdates = append(s.passwordUpdates, passwordUpdate{userID: id, hash: hash})
	return s.updatePasswordErr
}

var _ users.UserStore = (*stubUserStore)(nil)

type passwordUpdate struct {
	userID uuid.UUID
	hash   string
}

// stubAuthStore implements AuthStore with an in-memory map keyed by token hash.
type stubAuthStore struct {
	tokens     map[string]RefreshToken
	saveErr    error
	deletedAll []uuid.UUID

	// Password-reset side. resetTokens is ordered oldest-first, matching the
	// real store's "newest live token wins" read.
	resetTokens    []*PasswordResetToken
	consumedAt     map[uuid.UUID]time.Time
	lastSentAt     time.Time
	createResetErr error

	// Device sessions.
	sessions           []*Session
	revokedSessions    map[uuid.UUID]bool
	revokedAllSessions []uuid.UUID
	touched            []uuid.UUID
	createSessionErr   error
}

func newStubAuthStore() *stubAuthStore {
	return &stubAuthStore{
		tokens:          map[string]RefreshToken{},
		consumedAt:      map[uuid.UUID]time.Time{},
		revokedSessions: map[uuid.UUID]bool{},
	}
}

func (s *stubAuthStore) SaveRefreshToken(
	_ context.Context, userID, sessionID uuid.UUID, tokenHash string, expiresAt time.Time,
) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.tokens[tokenHash] = RefreshToken{
		ID: uuid.New(), UserID: userID, SessionID: sessionID,
		TokenHash: tokenHash, ExpiresAt: expiresAt,
	}
	return nil
}

// --- sessions ---

func (s *stubAuthStore) CreateSession(
	_ context.Context, userID uuid.UUID, info DeviceInfo,
) (*Session, error) {
	if s.createSessionErr != nil {
		return nil, s.createSessionErr
	}
	now := time.Now()
	sess := &Session{
		ID: uuid.New(), UserID: userID,
		DeviceName: nilIfBlank(info.DeviceName),
		Platform:   nilIfBlank(info.Platform),
		AppVersion: nilIfBlank(info.AppVersion),
		CreatedAt:  now, LastSeenAt: now,
	}
	s.sessions = append(s.sessions, sess)
	return sess, nil
}

func (s *stubAuthStore) IsSessionLive(_ context.Context, id uuid.UUID) (bool, error) {
	for _, sess := range s.sessions {
		if sess.ID == id {
			return !s.revokedSessions[id], nil
		}
	}
	return false, nil
}

func (s *stubAuthStore) ListSessions(_ context.Context, userID uuid.UUID) ([]Session, error) {
	out := []Session{}
	for _, sess := range s.sessions {
		if sess.UserID == userID && !s.revokedSessions[sess.ID] {
			out = append(out, *sess)
		}
	}
	return out, nil
}

func (s *stubAuthStore) TouchSession(_ context.Context, id uuid.UUID, info DeviceInfo) (bool, error) {
	for _, sess := range s.sessions {
		if sess.ID != id || s.revokedSessions[sess.ID] {
			continue
		}
		sess.LastSeenAt = time.Now()
		// COALESCE semantics: a blank header leaves the stored value alone.
		if v := nilIfBlank(info.DeviceName); v != nil {
			sess.DeviceName = v
		}
		if v := nilIfBlank(info.Platform); v != nil {
			sess.Platform = v
		}
		if v := nilIfBlank(info.AppVersion); v != nil {
			sess.AppVersion = v
		}
		s.touched = append(s.touched, id)
		return true, nil
	}
	return false, nil
}

func (s *stubAuthStore) RevokeSession(_ context.Context, userID, id uuid.UUID) (bool, error) {
	for _, sess := range s.sessions {
		if sess.ID != id || sess.UserID != userID || s.revokedSessions[sess.ID] {
			continue
		}
		s.revokedSessions[id] = true
		s.dropTokensForSession(id)
		return true, nil
	}
	return false, nil
}

func (s *stubAuthStore) RevokeOtherSessions(
	_ context.Context, userID, keepID uuid.UUID,
) (int64, error) {
	var count int64
	for _, sess := range s.sessions {
		if sess.UserID != userID || sess.ID == keepID || s.revokedSessions[sess.ID] {
			continue
		}
		s.revokedSessions[sess.ID] = true
		s.dropTokensForSession(sess.ID)
		count++
	}
	return count, nil
}

func (s *stubAuthStore) RevokeAllSessions(_ context.Context, userID uuid.UUID) error {
	s.revokedAllSessions = append(s.revokedAllSessions, userID)
	for _, sess := range s.sessions {
		if sess.UserID == userID {
			s.revokedSessions[sess.ID] = true
		}
	}
	for hash, t := range s.tokens {
		if t.UserID == userID {
			delete(s.tokens, hash)
		}
	}
	return nil
}

func (s *stubAuthStore) dropTokensForSession(id uuid.UUID) {
	for hash, t := range s.tokens {
		if t.SessionID == id {
			delete(s.tokens, hash)
		}
	}
}

func (s *stubAuthStore) GetRefreshToken(_ context.Context, tokenHash string) (*RefreshToken, error) {
	t, ok := s.tokens[tokenHash]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return &t, nil
}

func (s *stubAuthStore) DeleteRefreshToken(_ context.Context, tokenHash string) error {
	delete(s.tokens, tokenHash)
	return nil
}

func (s *stubAuthStore) DeleteAllUserRefreshTokens(_ context.Context, userID uuid.UUID) error {
	s.deletedAll = append(s.deletedAll, userID)
	for hash, t := range s.tokens {
		if t.UserID == userID {
			delete(s.tokens, hash)
		}
	}
	return nil
}

func (s *stubAuthStore) CreatePasswordResetToken(
	_ context.Context, userID uuid.UUID, codeHash string, expiresAt time.Time,
) error {
	if s.createResetErr != nil {
		return s.createResetErr
	}
	// Mirror the real store: a new code supersedes every live one.
	now := time.Now()
	for _, t := range s.resetTokens {
		if _, done := s.consumedAt[t.ID]; !done {
			s.consumedAt[t.ID] = now
		}
	}
	s.resetTokens = append(s.resetTokens, &PasswordResetToken{
		ID: uuid.New(), UserID: userID, CodeHash: codeHash,
		ExpiresAt: expiresAt, CreatedAt: now,
	})
	s.lastSentAt = now
	return nil
}

func (s *stubAuthStore) GetLivePasswordResetToken(
	_ context.Context, userID uuid.UUID,
) (*PasswordResetToken, error) {
	for i := len(s.resetTokens) - 1; i >= 0; i-- {
		t := s.resetTokens[i]
		if t.UserID != userID {
			continue
		}
		if _, done := s.consumedAt[t.ID]; done {
			continue
		}
		if t.ExpiresAt.Before(time.Now()) {
			continue
		}
		return t, nil
	}
	return nil, pgx.ErrNoRows
}

func (s *stubAuthStore) LastPasswordResetSentAt(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return s.lastSentAt, nil
}

func (s *stubAuthStore) IncrementPasswordResetAttempts(_ context.Context, id uuid.UUID) (int, error) {
	for _, t := range s.resetTokens {
		if t.ID == id {
			t.Attempts++
			return t.Attempts, nil
		}
	}
	return 0, pgx.ErrNoRows
}

func (s *stubAuthStore) ConsumePasswordResetToken(_ context.Context, id uuid.UUID) (bool, error) {
	if _, done := s.consumedAt[id]; done {
		return false, nil
	}
	s.consumedAt[id] = time.Now()
	return true, nil
}

func (s *stubAuthStore) ExpirePasswordResetTokens(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, t := range s.resetTokens {
		if t.UserID != userID {
			continue
		}
		if _, done := s.consumedAt[t.ID]; !done {
			s.consumedAt[t.ID] = now
		}
	}
	return nil
}

var _ AuthStore = (*stubAuthStore)(nil)

func newTestHandler(userStore users.UserStore, authStore AuthStore) *Handler {
	h, _ := newTestHandlerWithEmail(userStore, authStore)
	return h
}

func newTestHandlerWithEmail(
	userStore users.UserStore, authStore AuthStore,
) (*Handler, *email.MockSender) {
	cfg := &config.Config{GoogleClientID: "test-client-id"}
	sender := &email.MockSender{}
	return NewHandler(userStore, authStore, newTestJWTService(), cfg, sender), sender
}

// newAuthApp wires the auth routes the same way routes.Register does, minus
// the rate limiter. signedInAs, when non-nil, simulates the auth middleware for
// the authenticated routes. The optional sessionID stands in for the token's
// sid claim; omit it to exercise the pre-sessions path, where sid is uuid.Nil.
func newAuthApp(h *Handler, signedInAs *uuid.UUID, sessionID ...uuid.UUID) *fiber.App {
	app := fiber.New()
	authed := func(c *fiber.Ctx) error {
		if signedInAs != nil {
			c.Locals("user_id", *signedInAs)
		}
		if len(sessionID) > 0 {
			c.Locals("session_id", sessionID[0])
		}
		return c.Next()
	}
	app.Post("/auth/google", h.GoogleSignIn)
	app.Post("/auth/signup", h.EmailSignUp)
	app.Post("/auth/signin", h.EmailSignIn)
	app.Post("/auth/refresh", h.Refresh)
	app.Post("/auth/signout", authed, h.SignOut)
	app.Get("/api/v1/devices", authed, h.ListDevices)
	app.Delete("/api/v1/devices/others", authed, h.RevokeOtherDevices)
	app.Delete("/api/v1/devices/:id", authed, h.RevokeDevice)
	return app
}

// seedSession gives a stub store a live session plus a refresh token on it,
// the state a real sign-in would have left behind.
func seedSession(t *testing.T, store *stubAuthStore, userID uuid.UUID, name string) *Session {
	t.Helper()
	sess, err := store.CreateSession(
		context.Background(), userID, DeviceInfo{DeviceName: name},
	)
	require.NoError(t, err)
	return sess
}

func postJSON(t *testing.T, app *fiber.App, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func parseAuthResponse(t *testing.T, body string) authResponse {
	t.Helper()
	var resp authResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	return resp
}

func testUser(id uuid.UUID, email string) *users.User {
	name := "Tester"
	return &users.User{ID: id, Email: email, DisplayName: &name, Plan: "free"}
}

// --- EmailSignUp ---

func TestEmailSignUp_HappyPathIssuesTokens(t *testing.T) {
	userID := uuid.New()
	userStore := &stubUserStore{
		createFn: func(p users.CreateUserParams) (*users.User, error) {
			u := testUser(userID, p.Email)
			u.PasswordHash = p.PasswordHash
			return u, nil
		},
	}
	authStore := newStubAuthStore()
	app := newAuthApp(newTestHandler(userStore, authStore), nil)

	status, body := postJSON(t, app, "/auth/signup", `{"name":"Olek","email":"olek@example.com","password":"secret123"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	resp := parseAuthResponse(t, body)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
	assert.Equal(t, int64(15*60), resp.ExpiresIn)
	require.NotNil(t, resp.User)
	assert.Equal(t, "olek@example.com", resp.User.Email)

	// The issued refresh token must be persisted hashed, never raw.
	_, storedRaw := authStore.tokens[resp.RefreshToken]
	assert.False(t, storedRaw, "refresh token must not be stored in raw form")
	_, storedHashed := authStore.tokens[HashRefreshToken(resp.RefreshToken)]
	assert.True(t, storedHashed, "hashed refresh token must be persisted")

	// The password hash must be bcrypt, not the plaintext password.
	require.Len(t, userStore.createdParams, 1)
	hash := userStore.createdParams[0].PasswordHash
	require.NotNil(t, hash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(*hash), []byte("secret123")))

	// Sensitive fields are json:"-" — they must never appear in the response.
	assert.NotContains(t, body, "password")
	assert.NotContains(t, body, "google_id")
}

func TestEmailSignUp_NormalizesEmail(t *testing.T) {
	userStore := &stubUserStore{
		createFn: func(p users.CreateUserParams) (*users.User, error) {
			return testUser(uuid.New(), p.Email), nil
		},
	}
	app := newAuthApp(newTestHandler(userStore, newStubAuthStore()), nil)

	status, body := postJSON(t, app, "/auth/signup", `{"name":"Olek","email":"  OLek@ExAmple.COM ","password":"secret123"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	require.Len(t, userStore.byEmailArgs, 1)
	assert.Equal(t, "olek@example.com", userStore.byEmailArgs[0], "duplicate check must use the normalized email")
	require.Len(t, userStore.createdParams, 1)
	assert.Equal(t, "olek@example.com", userStore.createdParams[0].Email)
}

func TestEmailSignUp_DuplicateEmail(t *testing.T) {
	existing := testUser(uuid.New(), "olek@example.com")
	userStore := &stubUserStore{
		byEmailFn: func(string) (*users.User, error) { return existing, nil },
	}
	app := newAuthApp(newTestHandler(userStore, newStubAuthStore()), nil)

	status, body := postJSON(t, app, "/auth/signup", `{"name":"Olek","email":"olek@example.com","password":"secret123"}`)
	assert.Equal(t, fiber.StatusConflict, status)
	assert.Contains(t, body, "already exists")
}

func TestEmailSignUp_Validation(t *testing.T) {
	longName := strings.Repeat("a", 256)
	longPassword := strings.Repeat("a", 70) + "123" // 73 bytes, has a digit

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing name", `{"email":"a@b.co","password":"secret123"}`, "name is required"},
		{"whitespace-only name", `{"name":"   ","email":"a@b.co","password":"secret123"}`, "name is required"},
		{"name too long", `{"name":"` + longName + `","email":"a@b.co","password":"secret123"}`, "255 characters or fewer"},
		{"invalid email", `{"name":"O","email":"not-an-email","password":"secret123"}`, "invalid email"},
		{"password too short", `{"name":"O","email":"a@b.co","password":"ab1"}`, "at least 8 characters"},
		{"password without digit", `{"name":"O","email":"a@b.co","password":"abcdefgh"}`, "at least one number"},
		{"password over 72 bytes", `{"name":"O","email":"a@b.co","password":"` + longPassword + `"}`, "at most 72 characters"},
		{"malformed json", `{"name":`, "invalid request body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
			status, body := postJSON(t, app, "/auth/signup", tc.body)
			assert.Equal(t, fiber.StatusBadRequest, status)
			assert.Contains(t, body, tc.wantErr)
		})
	}
}

func TestEmailSignUp_CreateFailure(t *testing.T) {
	userStore := &stubUserStore{
		createFn: func(users.CreateUserParams) (*users.User, error) { return nil, errors.New("db down") },
	}
	app := newAuthApp(newTestHandler(userStore, newStubAuthStore()), nil)

	status, body := postJSON(t, app, "/auth/signup", `{"name":"O","email":"a@b.co","password":"secret123"}`)
	assert.Equal(t, fiber.StatusInternalServerError, status)
	assert.NotContains(t, body, "db down", "internal error details must not leak")
}

// --- EmailSignIn ---

func signInFixture(t *testing.T, password string) *users.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	hashStr := string(hash)
	u := testUser(uuid.New(), "olek@example.com")
	u.PasswordHash = &hashStr
	return u
}

func TestEmailSignIn_HappyPath(t *testing.T) {
	user := signInFixture(t, "secret123")
	userStore := &stubUserStore{
		byEmailFn: func(email string) (*users.User, error) {
			if email == user.Email {
				return user, nil
			}
			return nil, pgx.ErrNoRows
		},
	}
	authStore := newStubAuthStore()
	app := newAuthApp(newTestHandler(userStore, authStore), nil)

	status, body := postJSON(t, app, "/auth/signin", `{"email":"olek@example.com","password":"secret123"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	resp := parseAuthResponse(t, body)
	assert.NotEmpty(t, resp.AccessToken)
	require.NotNil(t, resp.User)
	assert.Equal(t, user.ID, resp.User.ID)
	assert.Len(t, authStore.tokens, 1)
}

func TestEmailSignIn_NormalizesEmail(t *testing.T) {
	user := signInFixture(t, "secret123")
	userStore := &stubUserStore{
		byEmailFn: func(email string) (*users.User, error) {
			if email == "olek@example.com" {
				return user, nil
			}
			return nil, pgx.ErrNoRows
		},
	}
	app := newAuthApp(newTestHandler(userStore, newStubAuthStore()), nil)

	status, _ := postJSON(t, app, "/auth/signin", `{"email":" OLEK@Example.com ","password":"secret123"}`)
	assert.Equal(t, fiber.StatusOK, status, "mixed-case email must reach the same account")
}

// Whether an email is registered must not be readable from the response. The
// three rejection paths — unknown email, Google-only account, wrong password —
// have to be byte-for-byte identical.
func TestEmailSignIn_RejectionsAreIndistinguishable(t *testing.T) {
	stores := map[string]*stubUserStore{
		"unknown email": {},
		"google-only": {byEmailFn: func(string) (*users.User, error) {
			u := signInFixture(t, "secret123")
			u.PasswordHash = nil
			return u, nil
		}},
		"wrong password": {byEmailFn: func(string) (*users.User, error) {
			return signInFixture(t, "secret123"), nil
		}},
	}

	bodies := map[string]string{}
	for name, store := range stores {
		app := newAuthApp(newTestHandler(store, newStubAuthStore()), nil)
		status, body := postJSON(t, app, "/auth/signin",
			`{"email":"probe@example.com","password":"wrong9999"}`)

		assert.Equal(t, fiber.StatusUnauthorized, status, name)
		bodies[name] = body
	}

	assert.Equal(t, bodies["unknown email"], bodies["google-only"],
		"a Google-only account must not be distinguishable from no account")
	assert.Equal(t, bodies["unknown email"], bodies["wrong password"],
		"a wrong password must not be distinguishable from an unregistered email")
	assert.Contains(t, bodies["unknown email"], "do not match our records")
}

func TestEmailSignIn_Failures(t *testing.T) {
	googleOnly := testUser(uuid.New(), "google@example.com") // PasswordHash nil

	tests := []struct {
		name       string
		store      *stubUserStore
		body       string
		wantStatus int
		wantErr    string
	}{
		{
			"unknown email",
			&stubUserStore{},
			`{"email":"ghost@example.com","password":"secret123"}`,
			fiber.StatusUnauthorized, invalidCredentialsMessage,
		},
		{
			"google-only account",
			&stubUserStore{byEmailFn: func(string) (*users.User, error) { return googleOnly, nil }},
			`{"email":"google@example.com","password":"secret123"}`,
			fiber.StatusUnauthorized, invalidCredentialsMessage,
		},
		{
			"wrong password",
			&stubUserStore{byEmailFn: func(string) (*users.User, error) { return signInFixture(t, "secret123"), nil }},
			`{"email":"olek@example.com","password":"wrong9999"}`,
			fiber.StatusUnauthorized, invalidCredentialsMessage,
		},
		{
			"missing fields",
			&stubUserStore{},
			`{"email":"","password":""}`,
			fiber.StatusBadRequest, "required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newAuthApp(newTestHandler(tc.store, newStubAuthStore()), nil)
			status, body := postJSON(t, app, "/auth/signin", tc.body)
			assert.Equal(t, tc.wantStatus, status)
			assert.Contains(t, body, tc.wantErr)
		})
	}
}

// --- Refresh ---

func TestRefresh_RotatesToken(t *testing.T) {
	user := testUser(uuid.New(), "olek@example.com")
	userStore := &stubUserStore{
		byIDFn: func(id uuid.UUID) (*users.User, error) {
			if id == user.ID {
				return user, nil
			}
			return nil, pgx.ErrNoRows
		},
	}
	authStore := newStubAuthStore()
	jwtSvc := newTestJWTService()
	raw, hash, err := jwtSvc.GenerateRefreshToken()
	require.NoError(t, err)
	sess := seedSession(t, authStore, user.ID, "iPhone 17")
	require.NoError(t, authStore.SaveRefreshToken(
		context.Background(), user.ID, sess.ID, hash, time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(userStore, authStore), nil)
	status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	resp := parseAuthResponse(t, body)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEqual(t, raw, resp.RefreshToken, "refresh must rotate the token")
	assert.Nil(t, resp.User, "refresh response must not include the user object")

	_, oldStillStored := authStore.tokens[hash]
	assert.False(t, oldStillStored, "old refresh token must be revoked on rotation")
	_, newStored := authStore.tokens[HashRefreshToken(resp.RefreshToken)]
	assert.True(t, newStored, "rotated refresh token must be persisted hashed")

	// The old token must now be unusable.
	status, body = postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)
	assert.Equal(t, fiber.StatusUnauthorized, status)
	assert.Contains(t, body, "invalid_refresh_token")
}

func TestRefresh_Failures(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
		status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"never-issued"}`)
		assert.Equal(t, fiber.StatusUnauthorized, status)
		assert.Contains(t, body, "invalid_refresh_token")
	})

	t.Run("expired token is rejected and deleted", func(t *testing.T) {
		authStore := newStubAuthStore()
		jwtSvc := newTestJWTService()
		raw, hash, err := jwtSvc.GenerateRefreshToken()
		require.NoError(t, err)
		expiredUser := uuid.New()
		sess := seedSession(t, authStore, expiredUser, "iPhone 17")
		require.NoError(t, authStore.SaveRefreshToken(
			context.Background(), expiredUser, sess.ID, hash, time.Now().Add(-time.Minute)))

		app := newAuthApp(newTestHandler(&stubUserStore{}, authStore), nil)
		status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)
		assert.Equal(t, fiber.StatusUnauthorized, status)
		assert.Contains(t, body, "invalid_refresh_token")
		_, still := authStore.tokens[hash]
		assert.False(t, still, "expired token must be purged")
	})

	t.Run("user deleted since issue", func(t *testing.T) {
		authStore := newStubAuthStore()
		jwtSvc := newTestJWTService()
		raw, hash, err := jwtSvc.GenerateRefreshToken()
		require.NoError(t, err)
		goneUser := uuid.New()
		sess := seedSession(t, authStore, goneUser, "iPhone 17")
		require.NoError(t, authStore.SaveRefreshToken(
			context.Background(), goneUser, sess.ID, hash, time.Now().Add(time.Hour)))

		app := newAuthApp(newTestHandler(&stubUserStore{}, authStore), nil) // byIDFn nil → ErrNoRows
		status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)
		assert.Equal(t, fiber.StatusUnauthorized, status)
		assert.Contains(t, body, "invalid_refresh_token")
	})

	t.Run("empty body", func(t *testing.T) {
		app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
		status, _ := postJSON(t, app, "/auth/refresh", `{}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
	})
}

// --- SignOut ---

// Signing out on one device leaves the others alone. It used to drop every
// refresh token the user had, so signing out on a phone silently signed out
// the tablet as well.
func TestSignOut_EndsOnlyTheCallingDevice(t *testing.T) {
	userID := uuid.New()
	authStore := newStubAuthStore()
	phone := seedSession(t, authStore, userID, "iPhone 17")
	tablet := seedSession(t, authStore, userID, "iPad Pro")
	require.NoError(t, authStore.SaveRefreshToken(
		context.Background(), userID, phone.ID, "hash-phone", time.Now().Add(time.Hour)))
	require.NoError(t, authStore.SaveRefreshToken(
		context.Background(), userID, tablet.ID, "hash-tablet", time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(&stubUserStore{}, authStore), &userID, phone.ID)
	status, body := postJSON(t, app, "/auth/signout", `{}`)
	require.Equal(t, fiber.StatusOK, status, body)

	assert.True(t, authStore.revokedSessions[phone.ID], "the calling device is signed out")
	assert.False(t, authStore.revokedSessions[tablet.ID], "the other device is left alone")
	_, phoneToken := authStore.tokens["hash-phone"]
	_, tabletToken := authStore.tokens["hash-tablet"]
	assert.False(t, phoneToken, "its refresh token goes with it")
	assert.True(t, tabletToken, "the other device keeps its token")
}

// An access token minted before sessions existed carries no sid. Revoking
// "the current session" would then be a no-op and leave the caller signed in
// server-side, so that case still clears everything.
func TestSignOut_WithoutASessionClaimRevokesEverything(t *testing.T) {
	userID := uuid.New()
	authStore := newStubAuthStore()
	phone := seedSession(t, authStore, userID, "iPhone 17")
	require.NoError(t, authStore.SaveRefreshToken(
		context.Background(), userID, phone.ID, "hash-phone", time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(&stubUserStore{}, authStore), &userID) // no sid
	status, body := postJSON(t, app, "/auth/signout", `{}`)
	require.Equal(t, fiber.StatusOK, status, body)
	assert.Equal(t, []uuid.UUID{userID}, authStore.revokedAllSessions)
	assert.Empty(t, authStore.tokens)
}

func TestSignOut_RequiresAuth(t *testing.T) {
	app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
	status, body := postJSON(t, app, "/auth/signout", `{}`)
	assert.Equal(t, fiber.StatusUnauthorized, status)
	assert.Contains(t, body, "unauthorized")
}

// --- GoogleSignIn ---

func googlePayload(subject, email, name, picture string) *idtoken.Payload {
	claims := map[string]interface{}{}
	if email != "" {
		claims["email"] = email
	}
	if name != "" {
		claims["name"] = name
	}
	if picture != "" {
		claims["picture"] = picture
	}
	return &idtoken.Payload{Subject: subject, Claims: claims}
}

func TestGoogleSignIn_CreatesNewUser(t *testing.T) {
	userStore := &stubUserStore{
		createFn: func(p users.CreateUserParams) (*users.User, error) {
			u := testUser(uuid.New(), p.Email)
			u.GoogleID = p.GoogleID
			return u, nil
		},
	}
	authStore := newStubAuthStore()
	h := newTestHandler(userStore, authStore)
	h.validate = func(_ context.Context, _, audience string) (*idtoken.Payload, error) {
		assert.Equal(t, "test-client-id", audience, "must validate against the configured client ID")
		return googlePayload("google-123", "OLek@Example.com", "Olek", "https://pic"), nil
	}

	app := newAuthApp(h, nil)
	status, body := postJSON(t, app, "/auth/google", `{"id_token":"valid"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	require.Len(t, userStore.createdParams, 1)
	created := userStore.createdParams[0]
	assert.Equal(t, "olek@example.com", created.Email, "google email must be normalized")
	require.NotNil(t, created.GoogleID)
	assert.Equal(t, "google-123", *created.GoogleID)

	resp := parseAuthResponse(t, body)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Len(t, authStore.tokens, 1)
}

func TestGoogleSignIn_ExistingUserRefreshesAvatar(t *testing.T) {
	existing := testUser(uuid.New(), "olek@example.com")
	userStore := &stubUserStore{
		byGoogleFn: func(string) (*users.User, error) { return existing, nil },
		updateFn: func(id uuid.UUID, p users.UpdateUserParams) (*users.User, error) {
			u := *existing
			u.AvatarURL = p.AvatarURL
			return &u, nil
		},
	}
	h := newTestHandler(userStore, newStubAuthStore())
	h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
		return googlePayload("google-123", "olek@example.com", "Olek", "https://new-pic"), nil
	}

	app := newAuthApp(h, nil)
	status, _ := postJSON(t, app, "/auth/google", `{"id_token":"valid"}`)
	require.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, 1, userStore.updateCalls, "changed avatar must trigger an update")
	assert.Empty(t, userStore.createdParams, "existing user must not be re-created")
}

func TestGoogleSignIn_Failures(t *testing.T) {
	t.Run("invalid id token", func(t *testing.T) {
		h := newTestHandler(&stubUserStore{}, newStubAuthStore())
		h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
			return nil, errors.New("token validation failed")
		}
		app := newAuthApp(h, nil)
		status, body := postJSON(t, app, "/auth/google", `{"id_token":"bogus"}`)
		assert.Equal(t, fiber.StatusUnauthorized, status)
		assert.Contains(t, body, "invalid id token")
	})

	t.Run("missing email claim", func(t *testing.T) {
		h := newTestHandler(&stubUserStore{}, newStubAuthStore())
		h.validate = func(context.Context, string, string) (*idtoken.Payload, error) {
			return googlePayload("google-123", "", "Olek", ""), nil
		}
		app := newAuthApp(h, nil)
		status, _ := postJSON(t, app, "/auth/google", `{"id_token":"valid"}`)
		assert.Equal(t, fiber.StatusBadGateway, status)
	})

	t.Run("missing id_token field", func(t *testing.T) {
		app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
		status, body := postJSON(t, app, "/auth/google", `{}`)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "id_token is required")
	})
}

package auth

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// newResetApp wires just the two reset routes, minus the rate limiter that
// routes.Register puts in front of them.
func newResetApp(h *Handler) *fiber.App {
	app := fiber.New()
	app.Post("/auth/forgot-password", h.ForgotPassword)
	app.Post("/auth/reset-password", h.ResetPassword)
	return app
}

// passwordUser is an email+password account; hash is a real bcrypt digest so
// the "old password stops working" assertions mean something.
func passwordUser(t *testing.T, id uuid.UUID, email, password string) *users.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	str := string(hash)
	u := testUser(id, email)
	u.PasswordHash = &str
	return u
}

func withUser(u *users.User) *stubUserStore {
	return &stubUserStore{
		byEmailFn: func(email string) (*users.User, error) {
			if u != nil && email == u.Email {
				return u, nil
			}
			return nil, pgx.ErrNoRows
		},
		byIDFn: func(id uuid.UUID) (*users.User, error) {
			if u != nil && id == u.ID {
				return u, nil
			}
			return nil, pgx.ErrNoRows
		},
	}
}

// --- ForgotPassword ---

func TestForgotPassword_MailsACodeToAKnownAddress(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)

	status, body := postJSON(t, newResetApp(h), "/auth/forgot-password",
		`{"email":"ana@example.com"}`)

	assert.Equal(t, fiber.StatusOK, status)
	assert.Contains(t, body, resetRequestedMessage)

	sent := sender.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, "ana@example.com", sent[0].To)
	assert.Equal(t, "Tester", sent[0].Name)
	assert.Len(t, sent[0].Code, 6, "the code is always six digits")
	assert.Regexp(t, `^\d{6}$`, sent[0].Code)
	assert.Equal(t, PasswordResetTTL, sent[0].TTL)

	// What got stored has to verify against what got mailed, or the code in the
	// inbox is useless.
	token, err := authStore.GetLivePasswordResetToken(t.Context(), user.ID)
	require.NoError(t, err)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(token.CodeHash), []byte(sent[0].Code)))
	assert.NotContains(t, token.CodeHash, sent[0].Code, "the code is hashed, not stored raw")
}

// The whole point of the generic response: this endpoint must not become the
// account-enumeration oracle that sign-in deliberately isn't.
func TestForgotPassword_AnswersTheSameForAnUnknownAddress(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	knownH, knownSender := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
	unknownH, unknownSender := newTestHandlerWithEmail(withUser(nil), newStubAuthStore())

	knownStatus, knownBody := postJSON(t, newResetApp(knownH), "/auth/forgot-password",
		`{"email":"ana@example.com"}`)
	unknownStatus, unknownBody := postJSON(t, newResetApp(unknownH), "/auth/forgot-password",
		`{"email":"nobody@example.com"}`)

	assert.Equal(t, knownStatus, unknownStatus)
	assert.Equal(t, knownBody, unknownBody)
	assert.Len(t, knownSender.Sent(), 1)
	assert.Empty(t, unknownSender.Sent(), "nothing is mailed to an address with no account")
}

func TestForgotPassword_TellsAGoogleAccountToUseGoogle(t *testing.T) {
	user := testUser(uuid.New(), "g@example.com") // no PasswordHash
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)

	status, body := postJSON(t, newResetApp(h), "/auth/forgot-password",
		`{"email":"g@example.com"}`)

	assert.Equal(t, fiber.StatusOK, status)
	assert.Contains(t, body, resetRequestedMessage)

	sent := sender.Sent()
	require.Len(t, sent, 1)
	assert.True(t, sent[0].Google)
	assert.Empty(t, sent[0].Code)

	// No token, so nobody can set a password on a Google account this way.
	_, err := authStore.GetLivePasswordResetToken(t.Context(), user.ID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestForgotPassword_WontMailTheSameAccountTwiceInAMinute(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	app := newResetApp(h)

	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	status, body := postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)

	assert.Equal(t, fiber.StatusOK, status, "the throttle is invisible to the caller")
	assert.Contains(t, body, resetRequestedMessage)
	assert.Len(t, sender.Sent(), 1, "the second request sends nothing")
}

func TestForgotPassword_RequestingAgainInvalidatesTheOlderCode(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	app := newResetApp(h)

	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	firstCode := sender.Last().Code

	// Step past the resend throttle rather than sleeping through it.
	authStore.lastSentAt = time.Now().Add(-2 * resendInterval)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	secondCode := sender.Last().Code
	require.NotEqual(t, firstCode, secondCode)

	status, body := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, firstCode))
	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Contains(t, body, invalidResetCodeMessage)

	status, _ = postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, secondCode))
	assert.Equal(t, fiber.StatusOK, status, "the newest code still works")
}

// A stored token whose email never left the building is a code nobody can use.
func TestForgotPassword_RetiresTheTokenWhenSendingFails(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	sender.SetError(errors.New("sendgrid down"))

	status, body := postJSON(t, newResetApp(h), "/auth/forgot-password",
		`{"email":"ana@example.com"}`)

	assert.Equal(t, fiber.StatusOK, status, "a delivery failure is not a caller-visible signal")
	assert.Contains(t, body, resetRequestedMessage)

	_, err := authStore.GetLivePasswordResetToken(t.Context(), user.ID)
	assert.ErrorIs(t, err, pgx.ErrNoRows, "no live code is left behind")
}

func TestForgotPassword_NormalisesTheEmail(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	h, sender := newTestHandlerWithEmail(withUser(user), newStubAuthStore())

	status, _ := postJSON(t, newResetApp(h), "/auth/forgot-password",
		`{"email":"  ANA@Example.COM  "}`)

	assert.Equal(t, fiber.StatusOK, status)
	require.Len(t, sender.Sent(), 1)
}

func TestForgotPassword_RejectsAMissingEmail(t *testing.T) {
	h, sender := newTestHandlerWithEmail(withUser(nil), newStubAuthStore())
	status, body := postJSON(t, newResetApp(h), "/auth/forgot-password", `{"email":"  "}`)

	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Contains(t, body, "email is required")
	assert.Empty(t, sender.Sent())
}

// --- ResetPassword ---

func TestResetPassword_SetsTheNewPasswordAndSignsThemIn(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	userStore := withUser(user)
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(userStore, authStore)
	app := newResetApp(h)

	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	code := sender.Last().Code

	status, body := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, code))

	require.Equal(t, fiber.StatusOK, status, body)
	resp := parseAuthResponse(t, body)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
	require.NotNil(t, resp.User)
	assert.Equal(t, user.ID, resp.User.ID)

	require.Len(t, userStore.passwordUpdates, 1)
	saved := userStore.passwordUpdates[0]
	assert.Equal(t, user.ID, saved.userID)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(saved.hash), []byte("newpass1")),
		"the stored hash verifies the new password")
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(saved.hash), []byte("oldpass1")),
		"and not the old one")
}

// A reset exists to lock out whoever prompted it. Leaving their refresh token
// alive would defeat the entire feature.
func TestResetPassword_RevokesEveryExistingSession(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	existing := seedSession(t, authStore, user.ID, "iPad Pro")
	require.NoError(t, authStore.SaveRefreshToken(
		t.Context(), user.ID, existing.ID, "an-existing-session", time.Now().Add(time.Hour)))

	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	app := newResetApp(h)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)

	status, _ := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, sender.Last().Code))

	require.Equal(t, fiber.StatusOK, status)
	assert.Contains(t, authStore.revokedAllSessions, user.ID)
	// The device is gone from the list too, not just stripped of its token —
	// otherwise a reset would leave a phantom entry on the devices screen.
	assert.True(t, authStore.revokedSessions[existing.ID])
	_, err := authStore.GetRefreshToken(t.Context(), "an-existing-session")
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	// The device that did the reset gets a session of its own, so it shows up
	// on the devices screen instead of the account looking signed in nowhere.
	live, err := authStore.ListSessions(t.Context(), user.ID)
	require.NoError(t, err)
	assert.Len(t, live, 1)
}

func TestResetPassword_CodeWorksOnlyOnce(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	h, sender := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
	app := newResetApp(h)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	code := sender.Last().Code
	reset := fmt.Sprintf(`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, code)

	first, _ := postJSON(t, app, "/auth/reset-password", reset)
	second, body := postJSON(t, app, "/auth/reset-password", reset)

	assert.Equal(t, fiber.StatusOK, first)
	assert.Equal(t, fiber.StatusBadRequest, second)
	assert.Contains(t, body, invalidResetCodeMessage)
}

func TestResetPassword_BurnsTheTokenAfterTooManyGuesses(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	app := newResetApp(h)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	realCode := sender.Last().Code

	wrong := "000000"
	if wrong == realCode {
		wrong = "111111"
	}
	for i := 0; i < maxResetAttempts; i++ {
		status, _ := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
			`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, wrong))
		require.Equal(t, fiber.StatusBadRequest, status)
	}

	// The real code is dead too — that's the point of burning the token.
	status, body := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, realCode))
	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Contains(t, body, invalidResetCodeMessage)
}

func TestResetPassword_RejectsAnExpiredCode(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	authStore := newStubAuthStore()
	h, sender := newTestHandlerWithEmail(withUser(user), authStore)
	app := newResetApp(h)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	code := sender.Last().Code

	require.Len(t, authStore.resetTokens, 1)
	authStore.resetTokens[0].ExpiresAt = time.Now().Add(-time.Second)

	status, body := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, code))
	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Contains(t, body, invalidResetCodeMessage)
}

// An unknown email and a wrong code have to be indistinguishable, or the reset
// endpoint leaks what sign-in refuses to.
func TestResetPassword_UnknownEmailLooksLikeAWrongCode(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	knownH, sender := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
	knownApp := newResetApp(knownH)
	_, _ = postJSON(t, knownApp, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	_ = sender.Last().Code

	wrongCodeStatus, wrongCodeBody := postJSON(t, knownApp, "/auth/reset-password",
		`{"email":"ana@example.com","code":"999999","password":"newpass1"}`)

	unknownH, _ := newTestHandlerWithEmail(withUser(nil), newStubAuthStore())
	unknownStatus, unknownBody := postJSON(t, newResetApp(unknownH), "/auth/reset-password",
		`{"email":"nobody@example.com","code":"999999","password":"newpass1"}`)

	assert.Equal(t, wrongCodeStatus, unknownStatus)
	assert.Equal(t, wrongCodeBody, unknownBody)
}

// Matching wording is only half of it. forgot-password answers every address
// the same, but it only stores a token for an address that has an account, so
// this endpoint only reached bcrypt when one existed: request a reset, submit a
// junk code, and a registered address took a bcrypt round while an unregistered
// one returned in microseconds. That is the enumeration oracle both endpoints
// exist to close, just spread over two requests.
//
// Asserted against a bcrypt round measured on this machine rather than a fixed
// duration, since the cost varies wildly across hardware.
func TestResetPassword_RejectionsAllPayForABcryptRound(t *testing.T) {
	probe := []byte("999999")
	start := time.Now()
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, probe)
	oneRound := time.Since(start)
	require.Greater(t, oneRound, time.Millisecond,
		"a bcrypt round should be measurable; the baseline is meaningless otherwise")

	// Half a round: comfortably above anything that skipped bcrypt (which lands
	// in microseconds) and well clear of scheduler noise.
	floor := oneRound / 2

	withLiveToken := func(t *testing.T) (*fiber.App, string) {
		t.Helper()
		user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
		h, _ := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
		app := newResetApp(h)
		_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
		return app, "ana@example.com"
	}

	noLiveToken := func(t *testing.T) (*fiber.App, string) {
		t.Helper()
		// A real account that never asked for a reset.
		user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
		h, _ := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
		return newResetApp(h), "ana@example.com"
	}

	unknownEmail := func(t *testing.T) (*fiber.App, string) {
		t.Helper()
		h, _ := newTestHandlerWithEmail(withUser(nil), newStubAuthStore())
		return newResetApp(h), "nobody@example.com"
	}

	tests := []struct {
		name  string
		setup func(*testing.T) (*fiber.App, string)
	}{
		{"unknown email", unknownEmail},
		{"real account with no live code", noLiveToken},
		{"real account with a live code, wrong guess", withLiveToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, email := tt.setup(t)
			body := fmt.Sprintf(
				`{"email":"%s","code":"999999","password":"newpass1"}`, email)

			start := time.Now()
			status, _ := postJSON(t, app, "/auth/reset-password", body)
			elapsed := time.Since(start)

			assert.Equal(t, fiber.StatusBadRequest, status)
			assert.Greater(t, elapsed, floor,
				"returned in %s against a %s bcrypt round — this path skipped the hash and is distinguishable by timing",
				elapsed, oneRound)
		})
	}
}

// The password rules are checked before the code is spent, so a rejected
// password doesn't cost the user their one use of the code.
func TestResetPassword_WeakPasswordDoesNotConsumeTheCode(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	h, sender := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
	app := newResetApp(h)
	_, _ = postJSON(t, app, "/auth/forgot-password", `{"email":"ana@example.com"}`)
	code := sender.Last().Code

	status, body := postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"short"}`, code))
	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Contains(t, body, "password must be at least 8 characters")

	status, _ = postJSON(t, app, "/auth/reset-password", fmt.Sprintf(
		`{"email":"ana@example.com","code":"%s","password":"newpass1"}`, code))
	assert.Equal(t, fiber.StatusOK, status, "the code survived the rejected attempt")
}

func TestResetPassword_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"missing email", `{"code":"123456","password":"newpass1"}`, "email and code are required"},
		{"missing code", `{"email":"ana@example.com","password":"newpass1"}`, "email and code are required"},
		{"no digit in password", `{"email":"ana@example.com","code":"123456","password":"password"}`,
			"password must contain at least one number"},
		{"malformed body", `not json`, "invalid request body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
			h, _ := newTestHandlerWithEmail(withUser(user), newStubAuthStore())
			status, body := postJSON(t, newResetApp(h), "/auth/reset-password", tt.body)
			assert.Equal(t, fiber.StatusBadRequest, status)
			assert.Contains(t, body, tt.wantErr)
		})
	}
}

func TestGenerateResetCode_IsAlwaysSixDigits(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < 500; i++ {
		code, err := generateResetCode()
		require.NoError(t, err)
		require.Len(t, code, 6, "a small draw must still be zero-padded")
		assert.Regexp(t, `^\d{6}$`, code)
		seen[code]++
	}
	// Not a randomness test — just a guard against a constant or a tiny range.
	assert.Greater(t, len(seen), 400, "codes should not repeat much across 500 draws")
}

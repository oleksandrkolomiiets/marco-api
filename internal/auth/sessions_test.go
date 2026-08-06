package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sendJSON(t *testing.T, app *fiber.App, method, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func parseDevices(t *testing.T, body string) []deviceResponse {
	t.Helper()
	var payload struct {
		Devices []deviceResponse `json:"devices"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	return payload.Devices
}

// --- ListDevices ---

func TestListDevices_MarksTheCallingDevice(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")
	tablet := seedSession(t, store, userID, "iPad Pro")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, phone.ID)
	status, body := sendJSON(t, app, "GET", "/api/v1/devices")
	require.Equal(t, fiber.StatusOK, status, body)

	devices := parseDevices(t, body)
	require.Len(t, devices, 2)

	byID := map[string]deviceResponse{}
	for _, d := range devices {
		byID[d.ID] = d
	}
	assert.True(t, byID[phone.ID.String()].Current)
	assert.False(t, byID[tablet.ID.String()].Current)
	require.NotNil(t, byID[phone.ID.String()].DeviceName)
	assert.Equal(t, "iPhone 17", *byID[phone.ID.String()].DeviceName)
}

// Someone else's devices are none of your business, and a session that has
// been signed out shouldn't linger on the screen.
func TestListDevices_ScopesToTheUserAndSkipsRevoked(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	mine := seedSession(t, store, userID, "iPhone 17")
	goneOfMine := seedSession(t, store, userID, "Old iPhone")
	seedSession(t, store, uuid.New(), "Someone else's phone")

	_, err := store.RevokeSession(context.Background(), userID, goneOfMine.ID)
	require.NoError(t, err)

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, mine.ID)
	_, body := sendJSON(t, app, "GET", "/api/v1/devices")

	devices := parseDevices(t, body)
	require.Len(t, devices, 1)
	assert.Equal(t, mine.ID.String(), devices[0].ID)
}

// An access token from before sessions existed has no sid, so nothing can be
// "this device" — but the list must still render rather than 500.
func TestListDevices_WithoutASessionClaimMarksNothingCurrent(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	seedSession(t, store, userID, "iPhone 17")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID)
	status, body := sendJSON(t, app, "GET", "/api/v1/devices")
	require.Equal(t, fiber.StatusOK, status)

	devices := parseDevices(t, body)
	require.Len(t, devices, 1)
	assert.False(t, devices[0].Current)
}

func TestListDevices_EmptyIsAnArrayNotNull(t *testing.T) {
	userID := uuid.New()
	app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), &userID)
	_, body := sendJSON(t, app, "GET", "/api/v1/devices")
	assert.Contains(t, body, `"devices":[]`)
}

func TestListDevices_RequiresAuth(t *testing.T) {
	app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
	status, body := sendJSON(t, app, "GET", "/api/v1/devices")
	assert.Equal(t, fiber.StatusUnauthorized, status)
	assert.Contains(t, body, "unauthorized")
}

// --- RevokeDevice ---

func TestRevokeDevice_SignsOutTheChosenDeviceOnly(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")
	tablet := seedSession(t, store, userID, "iPad Pro")
	require.NoError(t, store.SaveRefreshToken(
		context.Background(), userID, tablet.ID, "tablet-token", time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, phone.ID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/"+tablet.ID.String())
	require.Equal(t, fiber.StatusOK, status, body)
	assert.Contains(t, body, `"signed_out_self":false`)

	assert.True(t, store.revokedSessions[tablet.ID])
	assert.False(t, store.revokedSessions[phone.ID])
	// Revoking must take the refresh token with it, or the signed-out device
	// just mints itself a new access token on the next rotation.
	_, stillThere := store.tokens["tablet-token"]
	assert.False(t, stillThere)
}

// Revoking your own entry is just signing out, which is allowed — but the
// response has to say so or the app keeps using tokens the server discarded.
func TestRevokeDevice_CanSignOutTheCallingDevice(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, phone.ID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/"+phone.ID.String())
	require.Equal(t, fiber.StatusOK, status)
	assert.Contains(t, body, `"signed_out_self":true`)
	assert.True(t, store.revokedSessions[phone.ID])
}

// The store scopes by user_id, so another account's session id must read as
// "not found" rather than signing a stranger out.
func TestRevokeDevice_CannotTouchAnotherAccount(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	mine := seedSession(t, store, userID, "iPhone 17")
	theirs := seedSession(t, store, uuid.New(), "Someone else's phone")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, mine.ID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/"+theirs.ID.String())

	assert.Equal(t, fiber.StatusNotFound, status)
	assert.Contains(t, body, "device not found")
	assert.False(t, store.revokedSessions[theirs.ID])
}

func TestRevokeDevice_Failures(t *testing.T) {
	userID := uuid.New()

	t.Run("unparseable id", func(t *testing.T) {
		app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), &userID)
		status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/not-a-uuid")
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "invalid device id")
	})

	t.Run("already revoked", func(t *testing.T) {
		store := newStubAuthStore()
		gone := seedSession(t, store, userID, "Old iPhone")
		_, err := store.RevokeSession(context.Background(), userID, gone.ID)
		require.NoError(t, err)

		app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID)
		status, _ := sendJSON(t, app, "DELETE", "/api/v1/devices/"+gone.ID.String())
		assert.Equal(t, fiber.StatusNotFound, status)
	})

	t.Run("requires auth", func(t *testing.T) {
		app := newAuthApp(newTestHandler(&stubUserStore{}, newStubAuthStore()), nil)
		status, _ := sendJSON(t, app, "DELETE", "/api/v1/devices/"+uuid.New().String())
		assert.Equal(t, fiber.StatusUnauthorized, status)
	})
}

// --- RevokeOtherDevices ---

func TestRevokeOtherDevices_KeepsTheCallerSignedIn(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")
	tablet := seedSession(t, store, userID, "iPad Pro")
	laptop := seedSession(t, store, userID, "MacBook")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, phone.ID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/others")
	require.Equal(t, fiber.StatusOK, status, body)
	assert.Contains(t, body, `"signed_out":2`)

	assert.False(t, store.revokedSessions[phone.ID])
	assert.True(t, store.revokedSessions[tablet.ID])
	assert.True(t, store.revokedSessions[laptop.ID])
}

// Without a sid there is no "other" to scope to, and running it anyway would
// sign the caller out along with everything else.
func TestRevokeOtherDevices_RefusesWithoutASessionClaim(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/others")

	assert.Equal(t, fiber.StatusConflict, status)
	assert.Contains(t, body, "sign in again")
	assert.False(t, store.revokedSessions[phone.ID], "nothing was revoked")
}

// "others" must not be swallowed by the /devices/:id route and parsed as an id.
func TestRevokeOtherDevices_RouteWinsOverTheIdRoute(t *testing.T) {
	userID := uuid.New()
	store := newStubAuthStore()
	phone := seedSession(t, store, userID, "iPhone 17")

	app := newAuthApp(newTestHandler(&stubUserStore{}, store), &userID, phone.ID)
	status, body := sendJSON(t, app, "DELETE", "/api/v1/devices/others")

	assert.Equal(t, fiber.StatusOK, status)
	assert.NotContains(t, body, "invalid device id")
}

// --- device headers ---

func TestDeviceInfoFrom_ClampsWhatTheClientSends(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    DeviceInfo
	}{
		{
			"plain values",
			map[string]string{
				headerDeviceName: "iPhone 17",
				headerPlatform:   "iOS 26.5",
				headerAppVersion: "1.4.0",
			},
			DeviceInfo{DeviceName: "iPhone 17", Platform: "iOS 26.5", AppVersion: "1.4.0"},
		},
		{
			"trimmed",
			map[string]string{headerDeviceName: "  iPhone 17  "},
			DeviceInfo{DeviceName: "iPhone 17"},
		},
		{
			// The value is rendered straight onto the devices screen. A tab is
			// stripped by clampHeader; the newline never reaches it, because
			// the HTTP layer folds it to a space first. Both are gone by the
			// time this lands in the column, which is the point.
			"control characters do not survive",
			map[string]string{headerDeviceName: "iPhone\t17\nfake entry"},
			DeviceInfo{DeviceName: "iPhone17 fake entry"},
		},
		{
			"absent headers give empty strings",
			map[string]string{},
			DeviceInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			var got DeviceInfo
			app.Get("/probe", func(c *fiber.Ctx) error {
				got = deviceInfoFrom(c)
				return c.SendStatus(fiber.StatusOK)
			})
			req := httptest.NewRequest("GET", "/probe", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			_, err := app.Test(req, -1)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The columns are VARCHAR(120)/(60)/(40); an over-long header must be cut here
// rather than becoming a failed insert at sign-in.
func TestDeviceInfoFrom_TruncatesToTheColumnWidths(t *testing.T) {
	app := fiber.New()
	var got DeviceInfo
	app.Get("/probe", func(c *fiber.Ctx) error {
		got = deviceInfoFrom(c)
		return c.SendStatus(fiber.StatusOK)
	})
	req := httptest.NewRequest("GET", "/probe", nil)
	req.Header.Set(headerDeviceName, strings.Repeat("d", 500))
	req.Header.Set(headerPlatform, strings.Repeat("p", 500))
	req.Header.Set(headerAppVersion, strings.Repeat("v", 500))
	_, err := app.Test(req, -1)
	require.NoError(t, err)

	assert.Len(t, got.DeviceName, maxDeviceNameLen)
	assert.Len(t, got.Platform, maxPlatformLen)
	assert.Len(t, got.AppVersion, maxAppVersionLen)
}

// Multi-byte names must be cut on rune boundaries, not bytes, or the column
// gets a mangled trailing character.
func TestClampHeader_CutsOnRunes(t *testing.T) {
	got := clampHeader(strings.Repeat("é", 200), maxDeviceNameLen)
	assert.Equal(t, maxDeviceNameLen, len([]rune(got)))
	assert.True(t, strings.HasSuffix(got, "é"))
}

// Tested directly as well as through a request: the HTTP layer happens to fold
// newlines to spaces, so going only through fiber would leave clampHeader's own
// stripping unverified and free to regress.
func TestClampHeader_StripsControlCharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tab", "iPhone\t17", "iPhone17"},
		{"newline", "iPhone\n17", "iPhone17"},
		{"carriage return", "iPhone\r17", "iPhone17"},
		{"delete", "iPhone\x7f17", "iPhone17"},
		{"null", "iPhone\x0017", "iPhone17"},
		{"emoji survives", "Ana’s iPhone 📱", "Ana’s iPhone 📱"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampHeader(tt.in, maxDeviceNameLen))
		})
	}
}

// --- sign-in and refresh wiring ---

func TestEmailSignIn_RecordsTheDeviceFromHeaders(t *testing.T) {
	user := passwordUser(t, uuid.New(), "ana@example.com", "oldpass1")
	store := newStubAuthStore()
	h, _ := newTestHandlerWithEmail(withUser(user), store)
	app := newAuthApp(h, nil)

	req := httptest.NewRequest("POST", "/auth/signin",
		strings.NewReader(`{"email":"ana@example.com","password":"oldpass1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerDeviceName, "iPhone 17")
	req.Header.Set(headerPlatform, "iOS 26.5")
	req.Header.Set(headerAppVersion, "1.4.0")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	sessions, err := store.ListSessions(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.NotNil(t, sessions[0].DeviceName)
	assert.Equal(t, "iPhone 17", *sessions[0].DeviceName)
	require.NotNil(t, sessions[0].Platform)
	assert.Equal(t, "iOS 26.5", *sessions[0].Platform)
	require.NotNil(t, sessions[0].AppVersion)
	assert.Equal(t, "1.4.0", *sessions[0].AppVersion)
}

// The whole reason sessions exist: rotation must not look like a new device.
func TestRefresh_KeepsTheSameSessionAcrossRotation(t *testing.T) {
	userID := uuid.New()
	user := testUser(userID, "ana@example.com")
	userStore := withUser(user)
	store := newStubAuthStore()
	jwtSvc := newTestJWTService()

	raw, hash, err := jwtSvc.GenerateRefreshToken()
	require.NoError(t, err)
	sess := seedSession(t, store, userID, "iPhone 17")
	require.NoError(t, store.SaveRefreshToken(
		context.Background(), userID, sess.ID, hash, time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(userStore, store), nil)
	status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)
	require.Equal(t, fiber.StatusOK, status, body)

	sessions, err := store.ListSessions(context.Background(), userID)
	require.NoError(t, err)
	assert.Len(t, sessions, 1, "rotation must not create a second device")
	assert.Equal(t, sess.ID, sessions[0].ID)
	assert.Contains(t, store.touched, sess.ID, "last-seen is bumped")

	// The rotated token stays bound to the same session.
	rotated := parseAuthResponse(t, body)
	stored, ok := store.tokens[HashRefreshToken(rotated.RefreshToken)]
	require.True(t, ok)
	assert.Equal(t, sess.ID, stored.SessionID)
}

// A device signed out from elsewhere still holds a refresh token until it next
// talks to the server. That token must not bring the session back.
func TestRefresh_RejectsATokenWhoseSessionWasRevoked(t *testing.T) {
	userID := uuid.New()
	user := testUser(userID, "ana@example.com")
	store := newStubAuthStore()
	jwtSvc := newTestJWTService()

	raw, hash, err := jwtSvc.GenerateRefreshToken()
	require.NoError(t, err)
	sess := seedSession(t, store, userID, "iPad Pro")
	require.NoError(t, store.SaveRefreshToken(
		context.Background(), userID, sess.ID, hash, time.Now().Add(time.Hour)))

	// Revoke, then put the token back as though the tablet were offline when
	// it happened and still has its copy.
	_, err = store.RevokeSession(context.Background(), userID, sess.ID)
	require.NoError(t, err)
	require.NoError(t, store.SaveRefreshToken(
		context.Background(), userID, sess.ID, hash, time.Now().Add(time.Hour)))

	app := newAuthApp(newTestHandler(withUser(user), store), nil)
	status, body := postJSON(t, app, "/auth/refresh", `{"refresh_token":"`+raw+`"}`)

	assert.Equal(t, fiber.StatusUnauthorized, status)
	assert.Contains(t, body, "invalid_refresh_token")
}

package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"marco-api/internal/auth"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func newJWTService() *auth.JWTService {
	return auth.NewJWTService(testSecret, time.Minute, time.Hour)
}

// newAuthApp mounts the auth middleware in front of a probe handler that
// reports what the middleware put in Locals.
func newAuthApp(jwtSvc *auth.JWTService) *fiber.App {
	app := fiber.New()
	app.Use(NewAuthMiddleware(jwtSvc))
	app.Get("/protected", func(c *fiber.Ctx) error {
		plan, _ := c.Locals("plan").(string)
		return c.JSON(fiber.Map{
			"user_id": GetUserID(c).String(),
			"plan":    plan,
		})
	})
	return app
}

func TestNewAuthMiddleware_Rejections(t *testing.T) {
	jwtSvc := newJWTService()

	// Mint an expired token with a service that shares the secret but has a
	// negative access TTL, so ExpiresAt is already in the past.
	expiredSvc := auth.NewJWTService(testSecret, -time.Minute, time.Hour)
	expiredToken, err := expiredSvc.GenerateAccessToken(uuid.New(), "free")
	require.NoError(t, err)

	// A structurally valid token signed with the wrong secret.
	wrongKeySvc := auth.NewJWTService("ffffffffffffffffffffffffffffffff", time.Minute, time.Hour)
	wrongKeyToken, err := wrongKeySvc.GenerateAccessToken(uuid.New(), "free")
	require.NoError(t, err)

	tests := []struct {
		name      string
		header    string
		wantError string
	}{
		{
			name:      "missing header",
			header:    "",
			wantError: "missing or invalid authorization header",
		},
		{
			name:      "malformed header without Bearer prefix",
			header:    "Token abc123",
			wantError: "missing or invalid authorization header",
		},
		{
			name:      "lowercase bearer prefix",
			header:    "bearer abc123",
			wantError: "missing or invalid authorization header",
		},
		{
			name:      "garbage token",
			header:    "Bearer not.a.jwt",
			wantError: "invalid token",
		},
		{
			// net/http trims trailing whitespace from header values, so
			// "Bearer " arrives as "Bearer" and fails the prefix check.
			name:      "bare Bearer with no token",
			header:    "Bearer ",
			wantError: "missing or invalid authorization header",
		},
		{
			name:      "expired token",
			header:    "Bearer " + expiredToken,
			wantError: "invalid token",
		},
		{
			name:      "token signed with wrong secret",
			header:    "Bearer " + wrongKeyToken,
			wantError: "invalid token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newAuthApp(jwtSvc)

			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)

			var payload map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
			assert.Equal(t, map[string]string{"error": tt.wantError}, payload)
		})
	}
}

func TestNewAuthMiddleware_ValidToken(t *testing.T) {
	jwtSvc := newJWTService()
	userID := uuid.New()
	token, err := jwtSvc.GenerateAccessToken(userID, "free")
	require.NoError(t, err)

	// Capture what the downstream handler observes, in addition to the JSON
	// echo, so we assert on the real Locals types — not just serialization.
	var gotUserID uuid.UUID
	var gotPlan any
	app := fiber.New()
	app.Use(NewAuthMiddleware(jwtSvc))
	app.Get("/protected", func(c *fiber.Ctx) error {
		gotUserID = GetUserID(c)
		gotPlan = c.Locals("plan")
		return c.JSON(fiber.Map{"user_id": gotUserID.String()})
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, userID, gotUserID, "GetUserID must return the uuid from the token")
	assert.Equal(t, "free", gotPlan, `c.Locals("plan") must carry the token's plan claim`)

	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, userID.String(), payload["user_id"])
}

func TestGetUserID_NoLocals(t *testing.T) {
	// Without the middleware, GetUserID degrades to uuid.Nil instead of panicking.
	var got uuid.UUID
	app := fiber.New()
	app.Get("/open", func(c *fiber.Ctx) error {
		got = GetUserID(c)
		return c.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/open", nil), -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, uuid.Nil, got)
}

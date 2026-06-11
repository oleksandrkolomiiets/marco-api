package middleware

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRateLimitedApp() *fiber.App {
	app := fiber.New()
	app.Post("/api/v1/auth/sign-in", AuthRateLimit(), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})
	return app
}

func TestAuthRateLimit_Allows10ThenBlocks(t *testing.T) {
	app := newRateLimitedApp()

	// app.Test gives every request the same client IP (0.0.0.0), so all
	// requests share one sliding-window bucket.
	for i := 1; i <= 10; i++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/sign-in", nil)
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode,
			fmt.Sprintf("request %d of 10 must pass the limiter", i))
		resp.Body.Close()
	}

	// 11th request from the same IP within the window must be rejected.
	req := httptest.NewRequest("POST", "/api/v1/auth/sign-in", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode)

	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, map[string]string{"error": "too many attempts, try again in a minute"}, payload)
}

func TestAuthRateLimit_BucketsAreIndependentPerApp(t *testing.T) {
	// The limiter store is per-middleware-instance: a fresh app must start
	// with an empty bucket even right after another app exhausted its own.
	exhausted := newRateLimitedApp()
	for i := 0; i < 11; i++ {
		resp, err := exhausted.Test(httptest.NewRequest("POST", "/api/v1/auth/sign-in", nil), -1)
		require.NoError(t, err)
		resp.Body.Close()
	}

	fresh := newRateLimitedApp()
	resp, err := fresh.Test(httptest.NewRequest("POST", "/api/v1/auth/sign-in", nil), -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// AuthRateLimit throttles the credential endpoints (sign-in, sign-up, refresh,
// Google sign-in) to slow down brute-force and credential-stuffing attempts.
// 10 requests per minute per client IP, sliding window, in-memory — sufficient
// for a single API instance; swap the storage backend if the API is ever
// scaled horizontally.
func AuthRateLimit() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:               10,
		Expiration:        time.Minute,
		LimiterMiddleware: limiter.SlidingWindow{},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "too many attempts, try again in a minute"})
		},
	})
}

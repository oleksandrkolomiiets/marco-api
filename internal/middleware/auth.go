package middleware

import (
	"strings"

	"marco-api/internal/auth"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// NewAuthMiddleware validates the bearer token and, when the token names a
// device session, checks that session is still live.
//
// The session check costs one primary-key lookup per authenticated request.
// Without it an access token stays valid for its full 15 minutes after the
// device it belongs to is signed out from the devices screen — so "sign this
// device out" would leave whoever holds it a quarter of an hour of unrestricted
// access. Every authenticated route already queries Postgres at least once, so
// one indexed lookup is the cheap half of the trade.
func NewAuthMiddleware(jwtSvc *auth.JWTService, sessions auth.SessionChecker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing or invalid authorization header"})
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		claims, err := jwtSvc.ValidateAccessToken(tokenStr)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid token"})
		}

		// uuid.Nil means the token was minted before sessions existed. There is
		// nothing to check, and rejecting it would sign out everyone holding a
		// token from the previous deploy. They expire within 15 minutes.
		if claims.SessionID != uuid.Nil && sessions != nil {
			live, err := sessions.IsSessionLive(c.Context(), claims.SessionID)
			if err != nil {
				// A database problem is not an authentication failure; saying
				// 401 here would sign every user out during an outage.
				return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "internal server error"})
			}
			if !live {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "session_revoked"})
			}
		}

		c.Locals("user_id", claims.UserID)
		c.Locals("plan", claims.Plan)
		c.Locals("session_id", claims.SessionID)
		return c.Next()
	}
}

func GetUserID(c *fiber.Ctx) uuid.UUID {
	id, _ := c.Locals("user_id").(uuid.UUID)
	return id
}

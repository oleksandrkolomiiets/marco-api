package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	// PasswordResetTTL is how long a code stays usable. Long enough to go and
	// find the email, short enough that a six-digit code stays a six-digit
	// code's worth of risk.
	PasswordResetTTL = 15 * time.Minute

	// maxResetAttempts burns the token after this many wrong guesses. Six
	// digits is a million possibilities; five tries makes online guessing
	// hopeless without locking anyone out for a typo or two.
	maxResetAttempts = 5

	// resendInterval stops the endpoint being used to flood someone's inbox.
	// The per-IP rate limiter doesn't cover that on its own — the requests can
	// come from anywhere, but they all land on one victim's mailbox.
	resendInterval = 60 * time.Second

	// resetRequestedMessage is returned whether or not the address is
	// registered. Saying "no account with that email" here would undo the
	// enumeration fix on sign-in by moving the oracle one endpoint over.
	resetRequestedMessage = "If that email has an account, a reset code is on its way."

	invalidResetCodeMessage = "That code isn't valid. Request a new one."
)

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

// generateResetCode returns a zero-padded 6-digit code from crypto/rand.
// math/rand would be seeded predictably enough to guess.
func generateResetCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate reset code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ForgotPassword issues a reset code and mails it. It answers identically for
// a registered address, an unregistered one, a Google-only account and a
// too-soon retry — the only signal is in the inbox.
func (h *Handler) ForgotPassword(c *fiber.Ctx) error {
	var req forgotPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	req.Email = normalizeEmail(req.Email)
	if req.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email is required"})
	}

	accepted := c.JSON(fiber.Map{"message": resetRequestedMessage})

	user, err := h.users.GetUserByEmail(c.Context(), req.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return accepted
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Everything past here can fail without changing the answer. A caller must
	// not be able to tell a delivery failure from an unknown address, so these
	// are logged for the operator and swallowed for the client.
	lastSent, err := h.auth.LastPasswordResetSentAt(c.Context(), user.ID)
	if err != nil {
		log.Printf("password reset: read last sent for %s: %v", user.ID, err)
		return accepted
	}
	if !lastSent.IsZero() && time.Since(lastSent) < resendInterval {
		return accepted
	}

	if user.PasswordHash == nil {
		// Google-only account. Mail them the "use Google" note rather than
		// letting a reset silently attach a password to it.
		if err := h.email.SendPasswordResetForGoogleAccount(
			c.Context(), user.Email, displayNameOf(user.DisplayName),
		); err != nil {
			log.Printf("password reset: send google notice to %s: %v", user.ID, err)
		}
		return accepted
	}

	code, err := generateResetCode()
	if err != nil {
		log.Printf("password reset: generate code for %s: %v", user.ID, err)
		return accepted
	}
	// bcrypt, not SHA-256: see the note in migration 000018 — a fast digest of
	// a six-digit code is exhaustible from a database dump in seconds.
	codeHash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("password reset: hash code for %s: %v", user.ID, err)
		return accepted
	}

	if err := h.auth.CreatePasswordResetToken(
		c.Context(), user.ID, string(codeHash), time.Now().Add(PasswordResetTTL),
	); err != nil {
		log.Printf("password reset: persist token for %s: %v", user.ID, err)
		return accepted
	}

	if err := h.email.SendPasswordReset(
		c.Context(), user.Email, displayNameOf(user.DisplayName), code, PasswordResetTTL,
	); err != nil {
		// The token is already stored. Retire it rather than leaving a live
		// code nobody received — the user will request another one.
		log.Printf("password reset: send code to %s: %v", user.ID, err)
		if expireErr := h.auth.ExpirePasswordResetTokens(c.Context(), user.ID); expireErr != nil {
			log.Printf("password reset: expire undelivered token for %s: %v", user.ID, expireErr)
		}
	}
	return accepted
}

// ResetPassword exchanges a valid code for a new password and a fresh session.
func (h *Handler) ResetPassword(c *fiber.Ctx) error {
	var req resetPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	req.Email = normalizeEmail(req.Email)
	if req.Email == "" || req.Code == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email and code are required"})
	}
	// Validate the new password before spending the code, so a password that
	// was always going to be rejected doesn't cost the user their one use.
	if err := validatePassword(req.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user, err := h.users.GetUserByEmail(c.Context(), req.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		// Same wording as a bad code: this endpoint must not become the
		// enumeration oracle that sign-in no longer is.
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": invalidResetCodeMessage})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	token, err := h.auth.GetLivePasswordResetToken(c.Context(), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": invalidResetCodeMessage})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	if bcrypt.CompareHashAndPassword([]byte(token.CodeHash), []byte(req.Code)) != nil {
		attempts, incErr := h.auth.IncrementPasswordResetAttempts(c.Context(), token.ID)
		if incErr != nil {
			log.Printf("password reset: count attempt for %s: %v", user.ID, incErr)
		} else if attempts >= maxResetAttempts {
			if expErr := h.auth.ExpirePasswordResetTokens(c.Context(), user.ID); expErr != nil {
				log.Printf("password reset: burn guessed token for %s: %v", user.ID, expErr)
			}
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": invalidResetCodeMessage})
	}

	// Spend the code before touching the password. If two requests race with
	// the same valid code, only the one that wins this UPDATE proceeds.
	consumed, err := h.auth.ConsumePasswordResetToken(c.Context(), token.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if !consumed {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": invalidResetCodeMessage})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if err := h.users.UpdatePassword(c.Context(), user.ID, string(hash)); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Whoever knew the old password — including whoever the reset was prompted
	// by — loses every existing session. This is the point of a reset.
	if err := h.auth.DeleteAllUserRefreshTokens(c.Context(), user.ID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Re-read so the returned user carries the new password_hash state rather
	// than the stale row fetched above.
	user, err = h.users.GetUserByID(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	return h.issueTokens(c, user, true)
}

func displayNameOf(name *string) string {
	if name == nil {
		return ""
	}
	return *name
}

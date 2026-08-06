package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"marco-api/internal/config"
	"marco-api/internal/email"
	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
)

type IDTokenValidator func(ctx context.Context, idToken, audience string) (*idtoken.Payload, error)

type Handler struct {
	users    users.UserStore
	auth     AuthStore
	jwt      *JWTService
	cfg      *config.Config
	email    email.Sender
	validate IDTokenValidator
}

func NewHandler(
	userStore users.UserStore,
	authStore AuthStore,
	jwtSvc *JWTService,
	cfg *config.Config,
	sender email.Sender,
) *Handler {
	return &Handler{
		users:    userStore,
		auth:     authStore,
		jwt:      jwtSvc,
		cfg:      cfg,
		email:    sender,
		validate: idtoken.Validate,
	}
}

type googleSignInRequest struct {
	IDToken string `json:"id_token"`
}

type authResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresIn    int64       `json:"expires_in"`
	User         *users.User `json:"user,omitempty"`
}

func (h *Handler) GoogleSignIn(c *fiber.Ctx) error {
	var req googleSignInRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if req.IDToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id_token is required"})
	}

	payload, err := h.validate(c.Context(), req.IDToken, h.cfg.GoogleClientID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid id token"})
	}

	googleID := payload.Subject
	email, _ := payload.Claims["email"].(string)
	email = normalizeEmail(email)
	if googleID == "" || email == "" {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "incomplete google user info"})
	}
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)

	user, err := h.users.GetUserByGoogleID(c.Context(), googleID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		gid := googleID
		displayName := nilIfEmpty(name)
		avatar := nilIfEmpty(picture)
		user, err = h.users.CreateUser(c.Context(), users.CreateUserParams{
			GoogleID:    &gid,
			Email:       email,
			DisplayName: displayName,
			AvatarURL:   avatar,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create user"})
		}
	case err != nil:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	default:
		if picture != "" && (user.AvatarURL == nil || *user.AvatarURL != picture) {
			avatar := picture
			updated, uerr := h.users.UpdateUser(c.Context(), user.ID, users.UpdateUserParams{AvatarURL: &avatar})
			if uerr == nil {
				user = updated
			}
		}
	}

	return h.startSession(c, user, true)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil || req.RefreshToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	hash := HashRefreshToken(req.RefreshToken)
	stored, err := h.auth.GetRefreshToken(c.Context(), hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid_refresh_token"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if stored.ExpiresAt.Before(time.Now()) {
		_ = h.auth.DeleteRefreshToken(c.Context(), hash)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid_refresh_token"})
	}

	if err := h.auth.DeleteRefreshToken(c.Context(), hash); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	user, err := h.users.GetUserByID(c.Context(), stored.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid_refresh_token"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Rotation keeps the session and bumps its last-seen, which is the only
	// regular heartbeat the devices list gets. A session that was revoked from
	// another device fails here, so a refresh token that is still sitting in a
	// signed-out app cannot mint itself back to life.
	alive, err := h.auth.TouchSession(c.Context(), stored.SessionID, deviceInfoFrom(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if !alive {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid_refresh_token"})
	}

	return h.issueTokens(c, user, false, stored.SessionID)
}

func (h *Handler) SignOut(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	// Sign out this device, not every device. Before sessions existed this
	// revoked every refresh token the user had, so signing out on a phone also
	// signed out the tablet with no way to tell that had happened; "Sign out
	// all other devices" on the devices screen is the explicit way to do that
	// now. An access token minted before sessions existed carries no sid — for
	// those, fall back to the old behaviour rather than leaving the caller
	// signed in server-side. Access tokens last 15 minutes, so that ends fast.
	sessionID := SessionIDFrom(c)
	if sessionID == uuid.Nil {
		if err := h.auth.RevokeAllSessions(c.Context(), userID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"message": "signed out"})
	}
	if _, err := h.auth.RevokeSession(c.Context(), userID, sessionID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.JSON(fiber.Map{"message": "signed out"})
}

var emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type emailSignUpRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type emailSignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) EmailSignUp(c *fiber.Ctx) error {
	var req emailSignUpRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = normalizeEmail(req.Email)
	if req.Name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name is required"})
	}
	// users.display_name is VARCHAR(255) — reject early instead of surfacing a DB error.
	if utf8.RuneCountInString(req.Name) > 255 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name must be 255 characters or fewer"})
	}
	if !emailRE.MatchString(req.Email) || utf8.RuneCountInString(req.Email) > 255 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid email"})
	}
	if err := validatePassword(req.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	_, err := h.users.GetUserByEmail(c.Context(), req.Email)
	if err == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "an account with this email already exists"})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	hashStr := string(hash)
	name := req.Name

	user, err := h.users.CreateUser(c.Context(), users.CreateUserParams{
		PasswordHash: &hashStr,
		Email:        req.Email,
		DisplayName:  &name,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create user"})
	}

	return h.startSession(c, user, true)
}

func (h *Handler) EmailSignIn(c *fiber.Ctx) error {
	var req emailSignInRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	req.Email = normalizeEmail(req.Email)
	if req.Email == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email and password are required"})
	}

	user, err := h.users.GetUserByEmail(c.Context(), req.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return credentialsRejected(c, req.Password)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if user.PasswordHash == nil {
		// Account was created via Google, so there is no password to check —
		// answered the same way as a missing account so the two are
		// indistinguishable from outside.
		return credentialsRejected(c, req.Password)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		return credentialsRejected(c, "")
	}

	return h.startSession(c, user, true)
}

// invalidCredentialsMessage is the single answer to every failed sign-in. It
// used to be "no_account" for an unknown email and "wrong_password" for a known
// one, which let anyone probe which addresses are registered. Neutral wording,
// after Laravel's auth.failed.
const invalidCredentialsMessage = "These credentials do not match our records."

// dummyPasswordHash equalises response time. A missing account skipped bcrypt
// and answered in microseconds while a wrong password took ~60ms, so identical
// wording still told an attacker which emails exist. Comparing against a fixed
// hash makes both paths pay the same cost. Generated at bcrypt.DefaultCost, the
// same cost sign-up uses; it hashes a value no one can sign in with.
var dummyPasswordHash = []byte("$2a$10$Ov3kxx6/JOMgts6msQ.5TeZtl2YlSBftA.ZIxP4rJ89J06WwvW24e")

// credentialsRejected answers a failed sign-in. Pass the submitted password when
// no real hash was compared, so the burnt bcrypt round matches the timing of a
// genuine mismatch; pass "" when a comparison already ran.
func credentialsRejected(c *fiber.Ctx, unverifiedPassword string) error {
	if unverifiedPassword != "" {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(unverifiedPassword))
	}
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": invalidCredentialsMessage})
}

// normalizeEmail lowercases and trims an email so lookups and the UNIQUE
// constraint on users.email are effectively case-insensitive — without it,
// "Foo@x.com" and "foo@x.com" become two different accounts.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	// bcrypt only hashes the first 72 bytes and GenerateFromPassword errors on
	// longer input — reject with a 400 instead of letting that become a 500.
	if len(p) > 72 {
		return errors.New("password must be at most 72 characters")
	}
	hasDigit := false
	for _, r := range p {
		if unicode.IsDigit(r) {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return errors.New("password must contain at least one number")
	}
	return nil
}

// startSession is the sign-in path: a new device session, then tokens bound to
// it. Every caller that authenticates from scratch goes through here so no
// sign-in can quietly skip creating the session the devices list reads.
func (h *Handler) startSession(c *fiber.Ctx, user *users.User, includeUser bool) error {
	session, err := h.auth.CreateSession(c.Context(), user.ID, deviceInfoFrom(c))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to start session"})
	}
	return h.issueTokens(c, user, includeUser, session.ID)
}

func (h *Handler) issueTokens(
	c *fiber.Ctx, user *users.User, includeUser bool, sessionID uuid.UUID,
) error {
	accessToken, err := h.jwt.GenerateAccessToken(user.ID, user.Plan, sessionID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to issue access token"})
	}
	raw, hash, err := h.jwt.GenerateRefreshToken()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to issue refresh token"})
	}
	expiresAt := time.Now().Add(h.jwt.RefreshTTL())
	if err := h.auth.SaveRefreshToken(c.Context(), user.ID, sessionID, hash, expiresAt); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to persist refresh token"})
	}

	resp := authResponse{
		AccessToken:  accessToken,
		RefreshToken: raw,
		ExpiresIn:    int64(h.jwt.AccessTTL().Seconds()),
	}
	if includeUser {
		resp.User = user
	}
	return c.JSON(resp)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

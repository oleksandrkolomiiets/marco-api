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
	validate IDTokenValidator
}

func NewHandler(userStore users.UserStore, authStore AuthStore, jwtSvc *JWTService, cfg *config.Config) *Handler {
	return &Handler{
		users:    userStore,
		auth:     authStore,
		jwt:      jwtSvc,
		cfg:      cfg,
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

	return h.issueTokens(c, user, true)
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

	return h.issueTokens(c, user, false)
}

func (h *Handler) SignOut(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	if err := h.auth.DeleteAllUserRefreshTokens(c.Context(), userID); err != nil {
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

	return h.issueTokens(c, user, true)
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
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "no_account"})
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if user.PasswordHash == nil {
		// account was created via Google — no password set
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "no_account"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "wrong_password"})
	}

	return h.issueTokens(c, user, true)
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

func (h *Handler) issueTokens(c *fiber.Ctx, user *users.User, includeUser bool) error {
	accessToken, err := h.jwt.GenerateAccessToken(user.ID, user.Plan)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to issue access token"})
	}
	raw, hash, err := h.jwt.GenerateRefreshToken()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to issue refresh token"})
	}
	expiresAt := time.Now().Add(h.jwt.RefreshTTL())
	if err := h.auth.SaveRefreshToken(c.Context(), user.ID, hash, expiresAt); err != nil {
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

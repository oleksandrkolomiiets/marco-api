package users

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	store UserStore
}

func NewHandler(store UserStore) *Handler {
	return &Handler{store: store}
}

func (h *Handler) GetMe(c *fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	user, err := h.store.GetUserByID(c.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	return c.JSON(user)
}

type updateMeRequest struct {
	SkillLevel    *string `json:"skill_level"`
	DominantHand  *string `json:"dominant_hand"`
	CourtSide     *string `json:"court_side"`
	PlayFrequency *string `json:"play_frequency"`
	Goal          *string `json:"goal"`
}

var (
	validSkillLevel   = map[string]bool{"beginner": true, "intermediate": true, "advanced": true}
	validDominantHand = map[string]bool{"left": true, "right": true, "both": true}
	validCourtSide    = map[string]bool{"left": true, "right": true, "either": true}
)

func (r *updateMeRequest) validate() error {
	if r.SkillLevel != nil && !validSkillLevel[*r.SkillLevel] {
		return fmt.Errorf("skill_level must be one of: beginner, intermediate, advanced")
	}
	if r.DominantHand != nil && !validDominantHand[*r.DominantHand] {
		return fmt.Errorf("dominant_hand must be one of: left, right, both")
	}
	if r.CourtSide != nil && !validCourtSide[*r.CourtSide] {
		return fmt.Errorf("court_side must be one of: left, right, either")
	}
	// Length caps mirror the users table (play_frequency VARCHAR(20),
	// goal VARCHAR(50)) so over-long values fail with a 422 here instead of a
	// Postgres error surfacing as an opaque 500.
	if r.PlayFrequency != nil && utf8.RuneCountInString(*r.PlayFrequency) > 20 {
		return fmt.Errorf("play_frequency must be 20 characters or fewer")
	}
	if r.Goal != nil && utf8.RuneCountInString(*r.Goal) > 50 {
		return fmt.Errorf("goal must be 50 characters or fewer")
	}
	return nil
}

func (h *Handler) UpdateMe(c *fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req updateMeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if err := req.validate(); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}

	updated, err := h.store.UpdateUser(c.Context(), userID, UpdateUserParams{
		SkillLevel:    req.SkillLevel,
		DominantHand:  req.DominantHand,
		CourtSide:     req.CourtSide,
		PlayFrequency: req.PlayFrequency,
		Goal:          req.Goal,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	return c.JSON(updated)
}

func userIDFromCtx(c *fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals("user_id").(uuid.UUID)
	return id, ok && id != uuid.Nil
}

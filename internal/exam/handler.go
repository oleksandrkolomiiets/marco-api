package exam

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func currentUserID(c *fiber.Ctx) (uuid.UUID, error) {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return uuid.Nil, fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}
	return userID, nil
}

func (h *Handler) ListQuestions(c *fiber.Ctx) error {
	if _, err := currentUserID(c); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	qs, err := h.store.ListQuestions(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.JSON(qs)
}

type submitAnswerInput struct {
	QuestionID       string  `json:"question_id"`
	SelectedOptionID *string `json:"selected_option_id"`
}

type submitAttemptRequest struct {
	Answers []submitAnswerInput `json:"answers"`
}

func (h *Handler) SubmitAttempt(c *fiber.Ctx) error {
	userID, err := currentUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req submitAttemptRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	picks := make(map[uuid.UUID]uuid.UUID, len(req.Answers))
	for _, a := range req.Answers {
		qid, err := uuid.Parse(a.QuestionID)
		if err != nil {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "invalid question_id"})
		}
		if a.SelectedOptionID == nil {
			continue
		}
		oid, err := uuid.Parse(*a.SelectedOptionID)
		if err != nil {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "invalid selected_option_id"})
		}
		picks[qid] = oid
	}

	review, err := h.store.SubmitAttempt(c.Context(), userID, picks)
	if err != nil {
		if errors.Is(err, ErrPickNotOnQuestion) {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "selected_option_id does not belong to its question"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.Status(fiber.StatusCreated).JSON(review)
}

func (h *Handler) GetLatestAttempt(c *fiber.Ctx) error {
	userID, err := currentUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	review, err := h.store.GetLatestAttempt(c.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no attempt yet"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.JSON(review)
}

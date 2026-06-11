package lessons

import (
	"errors"
	"fmt"

	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	users   users.UserStore
	lessons LessonStore
}

func NewHandler(userStore users.UserStore, lessonStore LessonStore) *Handler {
	return &Handler{users: userStore, lessons: lessonStore}
}

var (
	validLevel  = map[string]bool{"beginner": true, "intermediate": true, "advanced": true}
	validStatus = map[string]bool{"viewed": true, "learned": true, "mastered": true}
)

func (h *Handler) currentUser(c *fiber.Ctx) (*users.User, error) {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
	}
	u, err := h.users.GetUserByID(c.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

func (h *Handler) ListLessons(c *fiber.Ctx) error {
	user, err := h.currentUser(c)
	if err != nil {
		return userErr(c, err)
	}

	level := c.Query("level")
	if level != "" && !validLevel[level] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "level must be one of: beginner, intermediate, advanced"})
	}

	lessons, err := h.lessons.ListLessons(c.Context(), level)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	progress, err := h.lessons.ListProgressByUserID(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	views := make([]LessonView, 0, len(lessons))
	for _, l := range lessons {
		v := LessonView{
			Lesson: l,
			Locked: !l.IsFree && user.Plan == "free",
		}
		if status, ok := progress[l.ID]; ok {
			s := status
			v.Progress = &s
		}
		views = append(views, v)
	}

	return c.JSON(views)
}

func (h *Handler) GetLesson(c *fiber.Ctx) error {
	user, err := h.currentUser(c)
	if err != nil {
		return userErr(c, err)
	}

	slug := c.Params("slug")
	lesson, err := h.lessons.GetLessonBySlug(c.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "lesson not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	if !lesson.IsFree && user.Plan == "free" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "upgrade_required"})
	}

	detail := LessonDetail{Lesson: lesson}
	progress, err := h.lessons.GetProgress(c.Context(), user.ID, lesson.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}
	if progress != nil {
		s := progress.Status
		detail.Progress = &s
	}

	return c.JSON(detail)
}

type updateProgressRequest struct {
	Status string `json:"status"`
}

func (h *Handler) UpdateProgress(c *fiber.Ctx) error {
	user, err := h.currentUser(c)
	if err != nil {
		return userErr(c, err)
	}

	var req updateProgressRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if !validStatus[req.Status] {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "status must be one of: viewed, learned, mastered"})
	}

	slug := c.Params("slug")
	lesson, err := h.lessons.GetLessonBySlug(c.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "lesson not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	if !lesson.IsFree && user.Plan == "free" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "upgrade_required"})
	}

	updated, err := h.lessons.UpsertProgress(c.Context(), user.ID, lesson.ID, req.Status)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	return c.JSON(updated)
}

func userErr(c *fiber.Ctx, err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
	}
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
}

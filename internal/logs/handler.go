package logs

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type matchStoreIface interface {
	CreateMatch(ctx context.Context, p CreateMatchParams) (MatchLog, error)
	UpdateMatch(ctx context.Context, p UpdateMatchParams) (MatchLog, error)
	ListMatches(ctx context.Context, userID uuid.UUID) ([]MatchLog, error)
	ListPartners(ctx context.Context, userID uuid.UUID) ([]PartnerSuggestion, error)
}

type LogsHandler struct {
	store matchStoreIface
}

func NewHandler(store matchStoreIface) *LogsHandler {
	return &LogsHandler{store: store}
}

type createMatchRequest struct {
	PlayedOn    string   `json:"played_on"`
	Result      *string  `json:"result"`
	Feeling     *string  `json:"feeling"`
	Note        *string  `json:"note"`
	PartnerName *string  `json:"partner_name"`
	Opponents   []string `json:"opponents"`
	// MessageID, when set, links the new match log to the assistant chat
	// message that prompted it so the chat UI can render a persistent "Logged"
	// state on the originating message.
	MessageID *string `json:"message_id,omitempty"`
}

func (h *LogsHandler) CreateMatch(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req createMatchRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	playedOn, opponents, errResp := validateMatchInput(&req)
	if errResp != nil {
		return c.Status(errResp.status).JSON(fiber.Map{"error": errResp.message})
	}

	var messageID *uuid.UUID
	if req.MessageID != nil && *req.MessageID != "" {
		parsed, err := uuid.Parse(*req.MessageID)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid message_id"})
		}
		messageID = &parsed
	}

	match, err := h.store.CreateMatch(c.Context(), CreateMatchParams{
		UserID:      userID,
		Result:      req.Result,
		Feeling:     req.Feeling,
		Note:        req.Note,
		PartnerName: req.PartnerName,
		Opponents:   opponents,
		PlayedOn:    playedOn,
		MessageID:   messageID,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create match log"})
	}

	return c.Status(fiber.StatusCreated).JSON(match)
}

type validationError struct {
	status  int
	message string
}

// validateMatchInput is the shared validation step between CreateMatch and
// UpdateMatch. It normalizes played_on, validates result/feeling/opponents,
// and returns the parsed date plus the cleaned opponents slice.
func validateMatchInput(req *createMatchRequest) (time.Time, []string, *validationError) {
	req.PlayedOn = strings.TrimSpace(req.PlayedOn)
	if req.PlayedOn == "" {
		return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "played_on is required"}
	}
	playedOn, err := time.Parse("2006-01-02", req.PlayedOn)
	if err != nil {
		return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "played_on must be YYYY-MM-DD"}
	}

	if req.Result != nil {
		switch *req.Result {
		case "won", "lost", "draw":
		default:
			return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "result must be won, lost, or draw"}
		}
	}
	if req.Feeling != nil && len(*req.Feeling) > 50 {
		return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "feeling must be 50 characters or fewer"}
	}
	// note is TEXT in the DB but flows into Marco's LLM context — cap it so a
	// single log can't bloat every future prompt.
	if req.Note != nil && utf8.RuneCountInString(*req.Note) > 2000 {
		return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "note must be 2000 characters or fewer"}
	}

	// Drop empty/whitespace partner_name so we store NULL instead of "" —
	// otherwise the blank string lingers in ListPartners and the chat prefill.
	if req.PartnerName != nil {
		trimmed := strings.TrimSpace(*req.PartnerName)
		if trimmed == "" {
			req.PartnerName = nil
		} else {
			req.PartnerName = &trimmed
		}
	}

	opponents := make([]string, 0, len(req.Opponents))
	for _, name := range req.Opponents {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 100 {
			return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "opponent name must be 100 characters or fewer"}
		}
		opponents = append(opponents, trimmed)
	}
	if len(opponents) > 3 {
		return time.Time{}, nil, &validationError{fiber.StatusBadRequest, "at most 3 opponents allowed"}
	}

	return playedOn, opponents, nil
}

func (h *LogsHandler) UpdateMatch(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	matchID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid match id"})
	}

	var req createMatchRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	playedOn, opponents, errResp := validateMatchInput(&req)
	if errResp != nil {
		return c.Status(errResp.status).JSON(fiber.Map{"error": errResp.message})
	}

	match, err := h.store.UpdateMatch(c.Context(), UpdateMatchParams{
		UserID:      userID,
		MatchID:     matchID,
		Result:      req.Result,
		Feeling:     req.Feeling,
		Note:        req.Note,
		PartnerName: req.PartnerName,
		Opponents:   opponents,
		PlayedOn:    playedOn,
	})
	if err != nil {
		if errors.Is(err, ErrMatchNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "match not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update match log"})
	}

	return c.JSON(match)
}

func (h *LogsHandler) ListMatches(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	matches, err := h.store.ListMatches(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list matches"})
	}

	return c.JSON(matches)
}

func (h *LogsHandler) ListPartners(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	partners, err := h.store.ListPartners(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list partners"})
	}

	return c.JSON(partners)
}

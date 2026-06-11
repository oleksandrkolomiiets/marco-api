package match_preparation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"marco-api/internal/anthropic"
	"marco-api/internal/marco"
)

// storeIface is the subset of *Store the handler uses, kept narrow so tests
// can stub it without a real DB.
type storeIface interface {
	Create(ctx context.Context, p CreateParams) (Preparation, error)
	Get(ctx context.Context, userID, id uuid.UUID) (Preparation, error)
	List(ctx context.Context, userID uuid.UUID) ([]Preparation, error)
	Update(ctx context.Context, p UpdateParams) (Preparation, error)
	ReplaceDrills(ctx context.Context, userID, preparationID uuid.UUID, inputs []DrillInput) (Preparation, error)
	SetDrillCompleted(ctx context.Context, userID, drillID uuid.UUID, completed bool) (Drill, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// assemblerIface mirrors marco.Assembler.Build so the suggestion endpoint can
// reuse the same user-context payload Marco sees in chat.
type assemblerIface interface {
	Build(ctx context.Context, userID uuid.UUID) (marco.UserContext, []anthropic.Message, error)
}

type Handler struct {
	store     storeIface
	client    anthropic.Client
	assembler assemblerIface
}

func NewHandler(store storeIface, client anthropic.Client, assembler assemblerIface) *Handler {
	return &Handler{store: store, client: client, assembler: assembler}
}

type drillRequest struct {
	Title           string `json:"title"`
	DurationSeconds int    `json:"duration_seconds"`
	// Completed is honoured on PUT /drills so the client can replace the queue
	// and the per-drill done flags in one round-trip. Ignored on create (a
	// fresh prep always starts with everything unchecked).
	Completed bool `json:"completed"`
}

type createRequest struct {
	ScheduledAt string         `json:"scheduled_at"`
	Opponents   []string       `json:"opponents"`
	PartnerName *string        `json:"partner_name"`
	Court       *string        `json:"court"`
	Note        *string        `json:"note"`
	Drills      []drillRequest `json:"drills"`
	// MessageID links this prep back to the assistant chat message that
	// spawned it (sent by the "Set up prep" tag). Optional — preps created
	// from the Match Prep tab omit it.
	MessageID *string `json:"message_id"`
}

func (h *Handler) Create(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req createRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	scheduledAt, opponents, partner, court, note, drills, errResp := validateCreate(&req)
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

	r, err := h.store.Create(c.Context(), CreateParams{
		UserID:      userID,
		ScheduledAt: scheduledAt,
		Opponents:   opponents,
		PartnerName: partner,
		Court:       court,
		Note:        note,
		Drills:      drills,
		MessageID:   messageID,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create preparation"})
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

func (h *Handler) List(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	items, err := h.store.List(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list preparation"})
	}
	return c.JSON(items)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}
	r, err := h.store.Get(c.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "preparation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load preparation"})
	}
	return c.JSON(r)
}

type updateRequest struct {
	ScheduledAt *string   `json:"scheduled_at"`
	Opponents   *[]string `json:"opponents"`
	PartnerName *string   `json:"partner_name"`
	Court       *string   `json:"court"`
	Note        *string   `json:"note"`
	MatchLogID  *string   `json:"match_log_id"`
	// PlanGrade can be "worked", "mixed", "missed", "" (clear). The "" form is
	// how the client un-grades a prep after a tap-by-mistake.
	PlanGrade *string `json:"plan_grade"`
	// PlayedAt is RFC3339 to mark a prep done, or "" to unmark it. Pass
	// "now" (a non-RFC3339 token we resolve server-side) to stamp the current
	// time — the typical path from the sheet's "Mark as played" button.
	PlayedAt *string `json:"played_at"`
}

func (h *Handler) Update(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var req updateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	params := UpdateParams{UserID: userID, ID: id}

	if req.ScheduledAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ScheduledAt)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "scheduled_at must be RFC3339"})
		}
		params.ScheduledAt = &t
	}
	if req.Opponents != nil {
		clean, errResp := cleanOpponents(*req.Opponents)
		if errResp != nil {
			return c.Status(errResp.status).JSON(fiber.Map{"error": errResp.message})
		}
		params.Opponents = &clean
	}
	if req.PartnerName != nil {
		trimmed := strings.TrimSpace(*req.PartnerName)
		if trimmed == "" {
			params.PartnerName = nil // store NULL
		} else if len(trimmed) > 100 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "partner_name must be 100 characters or fewer"})
		} else {
			params.PartnerName = &trimmed
		}
	}
	if req.Court != nil {
		trimmed := strings.TrimSpace(*req.Court)
		if trimmed == "" {
			params.Court = nil
		} else if len(trimmed) > 50 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "court must be 50 characters or fewer"})
		} else {
			params.Court = &trimmed
		}
	}
	if req.Note != nil {
		trimmed := strings.TrimSpace(*req.Note)
		if trimmed == "" {
			params.Note = nil
		} else if len(trimmed) > 200 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "note must be 200 characters or fewer"})
		} else {
			params.Note = &trimmed
		}
	}
	if req.MatchLogID != nil {
		if *req.MatchLogID == "" {
			params.ClearMatch = true
		} else {
			parsed, err := uuid.Parse(*req.MatchLogID)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid match_log_id"})
			}
			params.MatchLogID = &parsed
		}
	}
	if req.PlanGrade != nil {
		switch *req.PlanGrade {
		case "":
			params.ClearGrade = true
		case "worked", "mixed", "missed":
			grade := *req.PlanGrade
			params.PlanGrade = &grade
		default:
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "plan_grade must be worked, mixed, missed, or empty"})
		}
	}
	if req.PlayedAt != nil {
		switch *req.PlayedAt {
		case "":
			params.ClearPlayed = true
		case "now":
			t := time.Now().UTC()
			params.PlayedAt = &t
		default:
			t, err := time.Parse(time.RFC3339, *req.PlayedAt)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "played_at must be RFC3339, \"now\", or empty"})
			}
			params.PlayedAt = &t
		}
	}

	r, err := h.store.Update(c.Context(), params)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "preparation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update preparation"})
	}
	return c.JSON(r)
}

type replaceDrillsRequest struct {
	Drills []drillRequest `json:"drills"`
}

func (h *Handler) ReplaceDrills(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	var req replaceDrillsRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	drills, errResp := validateDrills(req.Drills)
	if errResp != nil {
		return c.Status(errResp.status).JSON(fiber.Map{"error": errResp.message})
	}

	r, err := h.store.ReplaceDrills(c.Context(), userID, id, drills)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "preparation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to update drills"})
	}
	return c.JSON(r)
}

type toggleDrillRequest struct {
	Completed bool `json:"completed"`
}

func (h *Handler) ToggleDrill(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	drillID, err := uuid.Parse(c.Params("drillId"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid drill id"})
	}

	var req toggleDrillRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}

	d, err := h.store.SetDrillCompleted(c.Context(), userID, drillID, req.Completed)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "drill not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to toggle drill"})
	}
	return c.JSON(d)
}

func (h *Handler) Delete(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}
	if err := h.store.Delete(c.Context(), userID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "preparation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete preparation"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SuggestDrills asks Marco for 3-5 drill ideas tailored to this prep. It
// reuses the chat assembler so Marco sees the same user context (level, hand,
// last match, etc.) it would in conversation. The model is forced to return a
// JSON array; the handler parses it and returns drill suggestions in our
// standard {title, duration_seconds} shape — same wire format as Drill inputs,
// so the client can append a suggestion to the queue with no transform.
func (h *Handler) SuggestDrills(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
	}

	r, err := h.store.Get(c.Context(), userID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "preparation not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load preparation"})
	}

	userCtx, _, err := h.assembler.Build(c.Context(), userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load context"})
	}

	suggestions, err := h.callSuggester(c.Context(), userCtx, r)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "failed to fetch suggestions"})
	}
	return c.JSON(suggestions)
}

type suggestion struct {
	Title           string `json:"title"`
	DurationSeconds int    `json:"duration_seconds"`
}

const suggesterSystemPrompt = `You are Marco, a padel coach helping a player prep for an upcoming match.
Suggest 3 to 5 short, specific drills the player can do in the days before the match.
Each drill is a single concrete exercise — name + duration in seconds.

Hard rules:
- Respond with a JSON array only. No prose, no markdown fences, no preamble.
- Each item: {"title": string, "duration_seconds": int}.
- Titles ≤ 60 characters, action-oriented (e.g. "Bandeja — paddle path", "Defensive stance reset").
- Durations between 60 and 1800 seconds (1 to 30 minutes). Pick realistic numbers.
- Avoid drills already in the player's current queue.
- Tailor to the player's level, dominant hand, court side, recent match notes, and the opponents named in the prep.`

func (h *Handler) callSuggester(ctx context.Context, userCtx marco.UserContext, r Preparation) ([]suggestion, error) {
	// Build a compact JSON brief so Marco has the prep on hand without us
	// duplicating the system prompt or chat history.
	brief := map[string]interface{}{
		"user_context": userCtx,
		"prep": map[string]interface{}{
			"scheduled_at":   r.ScheduledAt.Format(time.RFC3339),
			"opponents":      r.Opponents,
			"partner_name":   r.PartnerName,
			"court":          r.Court,
			"note":           r.Note,
			"current_drills": r.Drills,
		},
	}
	briefJSON, _ := json.Marshal(brief)

	chunks, errs := h.client.Stream(ctx, anthropic.StreamRequest{
		System:    suggesterSystemPrompt,
		Messages:  []anthropic.Message{{Role: anthropic.RoleUser, Content: string(briefJSON)}},
		MaxTokens: 600,
	})

	var finalText string
	for chunks != nil || errs != nil {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			if chunk.IsDone {
				finalText = chunk.FinalText
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("suggester stream: %w", err)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	parsed, err := parseSuggestions(finalText)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// parseSuggestions extracts the first JSON array in the model output, then
// validates each item. Marco is told to return JSON only, but we still scan
// past any stray prose by locating the outermost [ ... ] block — cheaper than
// re-prompting on a single misformatted reply.
func parseSuggestions(text string) ([]suggestion, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty suggester response")
	}
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in response: %q", truncate(text, 120))
	}
	raw := text[start : end+1]

	var items []suggestion
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("unmarshal suggestions: %w", err)
	}

	out := make([]suggestion, 0, len(items))
	for _, it := range items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		if len(title) > 60 {
			title = title[:60]
		}
		dur := it.DurationSeconds
		if dur < 60 {
			dur = 60
		}
		if dur > 1800 {
			dur = 1800
		}
		out = append(out, suggestion{Title: title, DurationSeconds: dur})
		if len(out) == 5 {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no valid suggestions in response")
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type validationError struct {
	status  int
	message string
}

func validateCreate(req *createRequest) (time.Time, []string, *string, *string, *string, []DrillInput, *validationError) {
	req.ScheduledAt = strings.TrimSpace(req.ScheduledAt)
	if req.ScheduledAt == "" {
		return time.Time{}, nil, nil, nil, nil, nil, &validationError{fiber.StatusBadRequest, "scheduled_at is required"}
	}
	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		return time.Time{}, nil, nil, nil, nil, nil, &validationError{fiber.StatusBadRequest, "scheduled_at must be RFC3339"}
	}

	opponents, errResp := cleanOpponents(req.Opponents)
	if errResp != nil {
		return time.Time{}, nil, nil, nil, nil, nil, errResp
	}

	var partner *string
	if req.PartnerName != nil {
		trimmed := strings.TrimSpace(*req.PartnerName)
		if trimmed != "" {
			if len(trimmed) > 100 {
				return time.Time{}, nil, nil, nil, nil, nil, &validationError{fiber.StatusBadRequest, "partner_name must be 100 characters or fewer"}
			}
			partner = &trimmed
		}
	}

	var court *string
	if req.Court != nil {
		trimmed := strings.TrimSpace(*req.Court)
		if trimmed != "" {
			if len(trimmed) > 50 {
				return time.Time{}, nil, nil, nil, nil, nil, &validationError{fiber.StatusBadRequest, "court must be 50 characters or fewer"}
			}
			court = &trimmed
		}
	}

	var note *string
	if req.Note != nil {
		trimmed := strings.TrimSpace(*req.Note)
		if trimmed != "" {
			if len(trimmed) > 200 {
				return time.Time{}, nil, nil, nil, nil, nil, &validationError{fiber.StatusBadRequest, "note must be 200 characters or fewer"}
			}
			note = &trimmed
		}
	}

	drills, errResp := validateDrills(req.Drills)
	if errResp != nil {
		return time.Time{}, nil, nil, nil, nil, nil, errResp
	}

	return scheduledAt, opponents, partner, court, note, drills, nil
}

func cleanOpponents(input []string) ([]string, *validationError) {
	out := make([]string, 0, len(input))
	for _, name := range input {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 100 {
			return nil, &validationError{fiber.StatusBadRequest, "opponent name must be 100 characters or fewer"}
		}
		out = append(out, trimmed)
	}
	if len(out) > 3 {
		return nil, &validationError{fiber.StatusBadRequest, "at most 3 opponents allowed"}
	}
	return out, nil
}

func validateDrills(input []drillRequest) ([]DrillInput, *validationError) {
	if len(input) > 20 {
		return nil, &validationError{fiber.StatusBadRequest, "at most 20 drills allowed"}
	}
	out := make([]DrillInput, 0, len(input))
	for i, d := range input {
		title := strings.TrimSpace(d.Title)
		if title == "" {
			return nil, &validationError{fiber.StatusBadRequest, fmt.Sprintf("drill %d: title is required", i+1)}
		}
		if len(title) > 200 {
			return nil, &validationError{fiber.StatusBadRequest, fmt.Sprintf("drill %d: title must be 200 characters or fewer", i+1)}
		}
		if d.DurationSeconds < 0 {
			return nil, &validationError{fiber.StatusBadRequest, fmt.Sprintf("drill %d: duration_seconds must be >= 0", i+1)}
		}
		if d.DurationSeconds > 7200 {
			return nil, &validationError{fiber.StatusBadRequest, fmt.Sprintf("drill %d: duration_seconds must be <= 7200", i+1)}
		}
		out = append(out, DrillInput{
			Title:           title,
			DurationSeconds: d.DurationSeconds,
			Completed:       d.Completed,
		})
	}
	return out, nil
}

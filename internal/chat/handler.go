package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"marco-api/internal/anthropic"
	"marco-api/internal/marco"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Assembler is the subset of *marco.Assembler the chat handler depends on,
// exposed as an interface so tests can stub it without a real database.
type Assembler interface {
	Build(ctx context.Context, userID uuid.UUID) (marco.UserContext, []anthropic.Message, error)
}

// TurnSaver persists a completed chat turn, supports feedback updates, soft
// delete, and can return a user's message history. *Store implements this;
// tests can stub it without a real database.
type TurnSaver interface {
	SaveTurn(ctx context.Context, userID uuid.UUID, userMessage, assistantMessage string, refs []marco.LessonRef) (uuid.UUID, uuid.UUID, error)
	SetFeedback(ctx context.Context, messageID uuid.UUID, userID uuid.UUID, score int8) error
	SoftDelete(ctx context.Context, messageID uuid.UUID, userID uuid.UUID) error
	GetHistory(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]Message, bool, error)
}

type Handler struct {
	assembler Assembler
	client    anthropic.Client
	store     TurnSaver
}

func NewHandler(assembler Assembler, client anthropic.Client, store TurnSaver) *Handler {
	return &Handler{assembler: assembler, client: client, store: store}
}

// Get returns one page of the authenticated user's message history, in
// chronological (ASC) order. Query params:
//   - limit  (optional, default 30, max 100) — page size
//   - before (optional, RFC3339Nano timestamp) — cursor; returns messages with
//     created_at < before. The client passes the oldest message's created_at
//     from the previous page to load the next page upward.
//
// Response includes `has_more` so the client can stop paginating when the
// conversation root is reached.
func (h *Handler) Get(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	limit := 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid limit"})
		}
		limit = n
	}

	var before *time.Time
	if raw := c.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid before timestamp"})
		}
		before = &t
	}

	messages, hasMore, err := h.store.GetHistory(c.Context(), userID, limit, before)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load messages"})
	}

	return c.JSON(fiber.Map{"messages": messages, "has_more": hasMore})
}

// maxMessageChars bounds a single user chat message (in runes, matching
// Postgres VARCHAR semantics).
const maxMessageChars = 4000

type postRequest struct {
	Message string `json:"message"`
}

// Post handles a chat turn: parse the request, assemble Marco's context from
// the DB, stream the model response back as SSE, then persist the user message
// and the assistant's final text (plus any parsed lesson refs) to Postgres so
// the next turn can read them back as conversation history.
//
// When Marco emits a [MATCH_LOG: ...] token, writeStream sends it as
// "event: match_log" before "event: done" so the client can open the log form
// pre-filled. The match is NOT saved here — the client calls POST /logs/match
// after the user confirms. The same flow handles [MATCH_PREP: ...] via an
// "event: match_prep" frame so the chat can open the prep sliding sheet.
func (h *Handler) Post(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req postRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "message is required"})
	}
	// Caps prompt size (and therefore Anthropic token spend) per turn.
	if utf8.RuneCountInString(req.Message) > maxMessageChars {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "message must be 4000 characters or fewer"})
	}

	streamCtx := c.UserContext()
	saveCtx := context.WithoutCancel(streamCtx)
	userMessage := req.Message

	userCtx, history, err := h.assembler.Build(streamCtx, userID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to load context"})
	}

	messages := append(history, anthropic.Message{Role: anthropic.RoleUser, Content: userMessage})
	streamReq := anthropic.StreamRequest{
		System:   marco.SystemPrompt(userCtx.ContextJSON()),
		Messages: messages,
	}

	chunks, errs := h.client.Stream(streamCtx, streamReq)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		finalText, ok := streamChunks(w, chunks, errs)
		if !ok {
			return
		}
		if finalText == "" {
			log.Printf("chat: stream ended without final text user_id=%s", userID)
			finishStream(w, uuid.Nil, uuid.Nil, "")
			return
		}
		refs := marco.ParseLessonRefs(finalText)
		userMsgID, assistantMsgID, err := h.store.SaveTurn(saveCtx, userID, userMessage, finalText, refs)
		if err != nil {
			log.Printf("failed to persist chat turn user_id=%s: %v", userID, err)
		}
		finishStream(w, userMsgID, assistantMsgID, finalText)
	})
	return nil
}

// Delete soft-deletes a message owned by the authenticated user. Used by the
// mobile client for both "hide message" and "retry" (which deletes the user
// and assistant messages of a turn before re-sending).
func (h *Handler) Delete(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	messageID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid message id"})
	}

	if err := h.store.SoftDelete(c.Context(), messageID, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "message not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete message"})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

type feedbackRequest struct {
	Score int8 `json:"score"`
}

// PatchFeedback sets thumbs-up (1) or thumbs-down (-1) on an assistant message.
func (h *Handler) PatchFeedback(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	messageID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid message id"})
	}

	var req feedbackRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if req.Score != 1 && req.Score != -1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "score must be 1 or -1"})
	}

	if err := h.store.SetFeedback(c.Context(), messageID, userID, req.Score); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "message not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to set feedback"})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

type chunkPayload struct {
	Text string `json:"text"`
}

type errorPayload struct {
	Error string `json:"error"`
}

type donePayload struct {
	UserMessageID      string `json:"user_message_id,omitempty"`
	AssistantMessageID string `json:"assistant_message_id,omitempty"`
}

// streamChunks pipes model chunks into SSE frames. It returns the assistant's
// final text and ok=true on successful completion (IsDone), or ok=false if the
// stream errored — the caller skips done emission in that case. Unlike the
// previous writeStream, the done event is NOT emitted here; finishStream below
// emits it after SaveTurn so the real message IDs can travel with it.
func streamChunks(w *bufio.Writer, chunks <-chan anthropic.StreamChunk, errs <-chan error) (string, bool) {
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
				continue
			}
			b, err := json.Marshal(chunkPayload{Text: chunk.Text})
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return "", false
			}
			if err := w.Flush(); err != nil {
				return "", false
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err == nil {
				continue
			}
			// Log the real error server-side; the client gets a generic message
			// so Anthropic SDK details (rate limits, request IDs) never leak.
			log.Printf("chat: stream error: %v", err)
			b, _ := json.Marshal(errorPayload{Error: "Marco is unavailable right now — please try again in a moment"})
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", b)
			_ = w.Flush()
			return "", false
		}
	}
	return finalText, true
}

// finishStream emits the trailing match_log / match_prep (if any) and done
// events. The done payload carries both message IDs so the mobile client can
// swap its temporary client-generated IDs for real UUIDs — needed for
// feedback/retry/hide to work on freshly sent messages without waiting for an
// app restart.
func finishStream(w *bufio.Writer, userMsgID, assistantMsgID uuid.UUID, finalText string) {
	if finalText != "" {
		if token := marco.ParseMatchLogToken(finalText); token != nil {
			if b, err := json.Marshal(token); err == nil {
				fmt.Fprintf(w, "event: match_log\ndata: %s\n\n", b)
				_ = w.Flush()
			}
		}
		if token := marco.ParseMatchPrepToken(finalText); token != nil {
			if b, err := json.Marshal(token); err == nil {
				fmt.Fprintf(w, "event: match_prep\ndata: %s\n\n", b)
				_ = w.Flush()
			}
		}
	}
	payload := donePayload{}
	if userMsgID != uuid.Nil {
		payload.UserMessageID = userMsgID.String()
	}
	if assistantMsgID != uuid.Nil {
		payload.AssistantMessageID = assistantMsgID.String()
	}
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte("{}")
	}
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", b)
	_ = w.Flush()
}

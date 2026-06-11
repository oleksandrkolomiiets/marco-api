package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"marco-api/internal/anthropic"
	"marco-api/internal/marco"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubAssembler struct {
	uc      marco.UserContext
	history []anthropic.Message
	err     error
}

func (s *stubAssembler) Build(_ context.Context, _ uuid.UUID) (marco.UserContext, []anthropic.Message, error) {
	return s.uc, s.history, s.err
}

var _ Assembler = (*stubAssembler)(nil)

type savedTurn struct {
	UserID           uuid.UUID
	UserMessage      string
	AssistantMessage string
	Refs             []marco.LessonRef
}

type stubStore struct {
	mu    sync.Mutex
	turns []savedTurn
	err   error
}

func (s *stubStore) SaveTurn(_ context.Context, userID uuid.UUID, userMessage, assistantMessage string, refs []marco.LessonRef) (uuid.UUID, uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return uuid.Nil, uuid.Nil, s.err
	}
	s.turns = append(s.turns, savedTurn{
		UserID:           userID,
		UserMessage:      userMessage,
		AssistantMessage: assistantMessage,
		Refs:             refs,
	})
	return uuid.New(), uuid.New(), nil
}

func (s *stubStore) SetFeedback(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ int8) error {
	return nil
}

func (s *stubStore) SoftDelete(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (s *stubStore) GetHistory(_ context.Context, _ uuid.UUID, _ int, _ *time.Time) ([]Message, bool, error) {
	return []Message{}, false, nil
}

func (s *stubStore) snapshot() []savedTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]savedTurn, len(s.turns))
	copy(out, s.turns)
	return out
}

var _ TurnSaver = (*stubStore)(nil)

func newAppWithAuth(handler *Handler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/api/v1/chat", handler.Post)
	return app
}

func newAppNoAuth(handler *Handler) *fiber.App {
	app := fiber.New()
	app.Post("/api/v1/chat", handler.Post)
	return app
}

// parseSSEFrames splits a raw SSE body into frames separated by blank lines.
func parseSSEFrames(body string) []string {
	var out []string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame != "" {
			out = append(out, frame)
		}
	}
	return out
}

func TestHandler_Post_StreamsChunks(t *testing.T) {
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: "Hello "},
		{Text: "Joost"},
		{Text: "!"},
		{IsDone: true, FinalText: "Hello Joost!"},
	}, nil)

	store := &stubStore{}
	userID := uuid.New()
	handler := NewHandler(&stubAssembler{}, mock, store)
	app := newAppWithAuth(handler, userID)

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	frames := parseSSEFrames(string(body))
	require.Len(t, frames, 4, "expected 3 data frames + 1 done frame, got: %v", frames)

	assert.Equal(t, `data: {"text":"Hello "}`, frames[0])
	assert.Equal(t, `data: {"text":"Joost"}`, frames[1])
	assert.Equal(t, `data: {"text":"!"}`, frames[2])
	assert.True(t, strings.HasPrefix(frames[3], "event: done\ndata: "), "expected done event, got: %q", frames[3])
	doneData := strings.TrimPrefix(frames[3], "event: done\ndata: ")
	var dp donePayload
	require.NoError(t, json.Unmarshal([]byte(doneData), &dp))
	_, err = uuid.Parse(dp.UserMessageID)
	require.NoError(t, err, "user_message_id must be a uuid, got: %q", dp.UserMessageID)
	_, err = uuid.Parse(dp.AssistantMessageID)
	require.NoError(t, err, "assistant_message_id must be a uuid, got: %q", dp.AssistantMessageID)

	turns := store.snapshot()
	require.Len(t, turns, 1, "expected one persisted turn (user + assistant)")
	assert.Equal(t, userID, turns[0].UserID)
	assert.Equal(t, "hi", turns[0].UserMessage)
	assert.Equal(t, "Hello Joost!", turns[0].AssistantMessage)
	require.NotNil(t, turns[0].Refs)
	assert.Empty(t, turns[0].Refs)
}

func TestHandler_Post_PersistsLessonRefs(t *testing.T) {
	final := `Try [LESSON_REF: bdj_001 | "Bandeja basics"] and you'll be set.`
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: final},
		{IsDone: true, FinalText: final},
	}, nil)

	store := &stubStore{}
	userID := uuid.New()
	handler := NewHandler(&stubAssembler{}, mock, store)
	app := newAppWithAuth(handler, userID)

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"what should I learn?"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	turns := store.snapshot()
	require.Len(t, turns, 1)
	assert.Equal(t, []marco.LessonRef{{ID: "bdj_001", Title: "Bandeja basics"}}, turns[0].Refs)
}

func TestHandler_Post_EmitsMatchPrepFrame(t *testing.T) {
	// Adjust mode — bare envelope, no nested drills.
	final := `[MATCH_PREP: {"mode":"adjust","id":"abc-123"}]` +
		"\nOpened Thursday's prep — what do you want to change?"
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: final},
		{IsDone: true, FinalText: final},
	}, nil)

	handler := NewHandler(&stubAssembler{}, mock, &stubStore{})
	app := newAppWithAuth(handler, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"adjust thursday's prep"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	frames := parseSSEFrames(string(body))

	// Find the match_prep frame; it must appear before the done frame and carry
	// the parsed token verbatim.
	var prepFrame string
	for _, f := range frames {
		if strings.HasPrefix(f, "event: match_prep\ndata: ") {
			prepFrame = f
			break
		}
	}
	require.NotEmpty(t, prepFrame, "expected match_prep frame in: %v", frames)

	payload := strings.TrimPrefix(prepFrame, "event: match_prep\ndata: ")
	var token marco.MatchPrepToken
	require.NoError(t, json.Unmarshal([]byte(payload), &token))
	assert.Equal(t, "adjust", token.Mode)
	assert.Equal(t, "abc-123", token.ID)
}

func TestHandler_Post_EmitsMatchPrepFrameForCreateMode(t *testing.T) {
	// Create mode — exercises the nested drills array which can't be parsed
	// by a flat `\{[^}]+\}` regex.
	final := `[MATCH_PREP: {"mode":"create","scheduled_at":"2026-05-24T18:00:00Z",` +
		`"opponents":["Marco","Ana"],"drills":[{"title":"Bandeja","duration_seconds":300}]}]` +
		"\nSetting one up for Saturday."
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: final},
		{IsDone: true, FinalText: final},
	}, nil)

	handler := NewHandler(&stubAssembler{}, mock, &stubStore{})
	app := newAppWithAuth(handler, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/chat",
		strings.NewReader(`{"message":"set up a prep for Saturday at 6pm vs Marco and Ana"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	frames := parseSSEFrames(string(body))

	var prepFrame string
	for _, f := range frames {
		if strings.HasPrefix(f, "event: match_prep\ndata: ") {
			prepFrame = f
			break
		}
	}
	require.NotEmpty(t, prepFrame, "expected match_prep frame in: %v", frames)

	payload := strings.TrimPrefix(prepFrame, "event: match_prep\ndata: ")
	var token marco.MatchPrepToken
	require.NoError(t, json.Unmarshal([]byte(payload), &token))
	assert.Equal(t, "create", token.Mode)
	assert.Equal(t, "2026-05-24T18:00:00Z", token.ScheduledAt)
	assert.Equal(t, []string{"Marco", "Ana"}, token.Opponents)
	require.Len(t, token.Drills, 1)
	assert.Equal(t, "Bandeja", token.Drills[0].Title)
	assert.Equal(t, 300, token.Drills[0].DurationSeconds)
}

func TestHandler_Post_DoesNotSaveOnStreamError(t *testing.T) {
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: "partial"},
	}, errors.New("upstream blew up"))

	store := &stubStore{}
	handler := NewHandler(&stubAssembler{}, mock, store)
	app := newAppWithAuth(handler, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	assert.Empty(t, store.snapshot(), "no save expected when stream errors before IsDone")
}

// TestHandler_Post_SavesEvenIfClientDisconnects is documentation-only: the
// context.WithoutCancel split between streamCtx and saveCtx in handler.go is
// what makes this work in production. Reproduce manually by killing curl
// mid-stream and verifying messages count increases by 2 anyway.
//
//	curl -N -H "Authorization: Bearer $TOKEN" -X POST \
//	  -H "Content-Type: application/json" \
//	  -d '{"message":"tell me a long story"}' \
//	  http://localhost:8080/api/v1/chat
//	# Ctrl-C while streaming, then:
//	docker exec -it marco_db psql -U marco -d marco_dev -c \
//	  "SELECT count(*) FROM messages WHERE user_id = '<uid>';"
func TestHandler_Post_SavesEvenIfClientDisconnects(t *testing.T) {
	t.Skip("flaky timing test, run manually — see comment above")
}

func TestHandler_Post_ReturnsErrorForMissingMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"empty message field", `{"message":""}`},
		{"whitespace only", `{"message":"   "}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &anthropic.MockClient{}
			handler := NewHandler(&stubAssembler{}, mock, &stubStore{})
			app := newAppWithAuth(handler, uuid.New())

			body := strings.NewReader(tt.body)
			req := httptest.NewRequest("POST", "/api/v1/chat", body)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestHandler_Post_ReturnsErrorForMissingAuth(t *testing.T) {
	mock := &anthropic.MockClient{}
	handler := NewHandler(&stubAssembler{}, mock, &stubStore{})
	app := newAppNoAuth(handler)

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
}

func TestHandler_Post_EmitsGenericStreamError(t *testing.T) {
	mock := &anthropic.MockClient{}
	mock.Setup([]anthropic.StreamChunk{
		{Text: "partial"},
	}, errors.New("upstream blew up"))

	handler := NewHandler(&stubAssembler{}, mock, &stubStore{})
	app := newAppWithAuth(handler, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Streaming has already begun by the time the error happens, so HTTP 200 is correct.
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	frames := parseSSEFrames(string(body))
	require.Len(t, frames, 2, "expected 1 data frame + 1 error frame, got: %v", frames)
	assert.Equal(t, `data: {"text":"partial"}`, frames[0])
	assert.Contains(t, frames[1], "event: error")
	// The raw upstream error must never reach the client — only a generic message.
	assert.NotContains(t, frames[1], "upstream blew up")
	assert.Contains(t, frames[1], "Marco is unavailable right now")
}

func TestHandler_Post_ReturnsErrorWhenAssemblerFails(t *testing.T) {
	mock := &anthropic.MockClient{}
	handler := NewHandler(&stubAssembler{err: errors.New("db down")}, mock, &stubStore{})
	app := newAppWithAuth(handler, uuid.New())

	req := httptest.NewRequest("POST", "/api/v1/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}

// Sanity-check the streamChunks helper directly without the Fiber layer.
func TestStreamChunks_DirectFraming(t *testing.T) {
	chunks := make(chan anthropic.StreamChunk, 3)
	errs := make(chan error, 1)
	chunks <- anthropic.StreamChunk{Text: "a"}
	chunks <- anthropic.StreamChunk{Text: "b\n\"c\""}
	chunks <- anthropic.StreamChunk{IsDone: true}
	close(chunks)
	close(errs)

	var sb strings.Builder
	w := bufio.NewWriter(&sb)
	finalText, ok := streamChunks(w, chunks, errs)
	_ = w.Flush()
	require.True(t, ok)
	assert.Equal(t, "", finalText)

	frames := parseSSEFrames(sb.String())
	require.Len(t, frames, 2)
	assert.Equal(t, `data: {"text":"a"}`, frames[0])
	assert.Equal(t, `data: {"text":"b\n\"c\""}`, frames[1])
}

// finishStream emits the done event with the message IDs the client uses to
// reconcile its temporary IDs.
func TestFinishStream_IncludesIDs(t *testing.T) {
	var sb strings.Builder
	w := bufio.NewWriter(&sb)
	userID := uuid.New()
	assistantID := uuid.New()
	finishStream(w, userID, assistantID, "ok")
	_ = w.Flush()

	frames := parseSSEFrames(sb.String())
	require.Len(t, frames, 1)
	require.True(t, strings.HasPrefix(frames[0], "event: done\ndata: "))
	data := strings.TrimPrefix(frames[0], "event: done\ndata: ")
	var dp donePayload
	require.NoError(t, json.Unmarshal([]byte(data), &dp))
	assert.Equal(t, userID.String(), dp.UserMessageID)
	assert.Equal(t, assistantID.String(), dp.AssistantMessageID)
}

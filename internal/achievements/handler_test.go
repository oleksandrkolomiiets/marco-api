package achievements

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubStore struct {
	summary   *Summary
	err       error
	gotUserID uuid.UUID
}

func (s *stubStore) GetForUser(_ context.Context, userID uuid.UUID) (*Summary, error) {
	s.gotUserID = userID
	return s.summary, s.err
}

var _ Store = (*stubStore)(nil)

// newApp builds a fiber app with the List route. localsValue is injected as
// c.Locals("user_id") when non-nil, mirroring the auth middleware contract.
func newApp(h *Handler, localsValue any) *fiber.App {
	app := fiber.New()
	if localsValue != nil {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user_id", localsValue)
			return c.Next()
		})
	}
	app.Get("/api/v1/achievements", h.List)
	return app
}

func TestList_HappyPath(t *testing.T) {
	userID := uuid.New()
	unlockedAt := "2026-06-01T10:00:00Z"
	summary := &Summary{
		Unlocked: 1,
		Total:    10,
		Achievements: []Achievement{
			{
				Slug:          "first-lesson",
				Title:         "First lesson",
				Description:   "Every coach has a first day.",
				Criteria:      "Mark any lesson as Learned or Mastered.",
				ProgressLabel: "1 lessons learned",
				Icon:          "▸",
				Accent:        "teal",
				Unlocked:      true,
				Progress:      1,
				Target:        1,
				UnlockedAt:    &unlockedAt,
			},
			{
				Slug:          "match-diarist",
				Title:         "Match diarist",
				Description:   "Ten logged matches.",
				Criteria:      "Log ten matches in your diary.",
				ProgressLabel: "3 / 10 matches logged",
				Icon:          "10",
				Accent:        "teal",
				Unlocked:      false,
				Progress:      3,
				Target:        10,
				UnlockedAt:    nil,
			},
		},
	}
	store := &stubStore{summary: summary}
	app := newApp(NewHandler(store), userID)

	req := httptest.NewRequest("GET", "/api/v1/achievements", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	assert.Equal(t, userID, store.gotUserID, "store must be queried with the authenticated user")

	// Decode into a generic map to pin the exact JSON shape (field names and
	// nesting), not just the Go struct round-trip.
	var got map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	assert.Equal(t, float64(1), got["unlocked"])
	assert.Equal(t, float64(10), got["total"])

	achievements, ok := got["achievements"].([]any)
	require.True(t, ok, "achievements must be a JSON array")
	require.Len(t, achievements, 2)

	first, ok := achievements[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "first-lesson", first["slug"])
	assert.Equal(t, "First lesson", first["title"])
	assert.Equal(t, "Every coach has a first day.", first["description"])
	assert.Equal(t, "Mark any lesson as Learned or Mastered.", first["criteria"])
	assert.Equal(t, "1 lessons learned", first["progress_label"])
	assert.Equal(t, "▸", first["icon"])
	assert.Equal(t, "teal", first["accent"])
	assert.Equal(t, true, first["unlocked"])
	assert.Equal(t, float64(1), first["progress"])
	assert.Equal(t, float64(1), first["target"])
	assert.Equal(t, unlockedAt, first["unlocked_at"])

	second, ok := achievements[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "match-diarist", second["slug"])
	assert.Equal(t, false, second["unlocked"])
	assert.Equal(t, float64(3), second["progress"])
	assert.Equal(t, float64(10), second["target"])
	assert.Nil(t, second["unlocked_at"], "locked achievements serialize unlocked_at as null")
}

func TestList_StoreError(t *testing.T) {
	store := &stubStore{err: errors.New("db down")}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/achievements", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, map[string]string{"error": "internal server error"}, payload)
}

func TestList_Unauthorized(t *testing.T) {
	tests := []struct {
		name   string
		locals any
	}{
		{"no user_id local", nil},
		{"nil uuid", uuid.Nil},
		{"wrong type", "not-a-uuid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubStore{summary: &Summary{}}
			app := newApp(NewHandler(store), tt.locals)

			req := httptest.NewRequest("GET", "/api/v1/achievements", nil)
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)

			var payload map[string]string
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
			assert.Equal(t, map[string]string{"error": "unauthorized"}, payload)
			assert.Equal(t, uuid.Nil, store.gotUserID, "store must not be called without auth")
		})
	}
}

// compute is pure Go, so pin one representative unlock rule through the
// handler's JSON path: the summary the handler returns is exactly what the
// store produced — the handler must not re-derive or mutate it.
func TestList_PassesSummaryThroughVerbatim(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC).Format(time.RFC3339)
	summary := &Summary{
		Unlocked: 0,
		Total:    10,
		Achievements: []Achievement{
			{Slug: "comeback-kid", ProgressLabel: "No comebacks yet", UnlockedAt: &now},
		},
	}
	store := &stubStore{summary: summary}
	app := newApp(NewHandler(store), uuid.New())

	req := httptest.NewRequest("GET", "/api/v1/achievements", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var got Summary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, *summary, got)
}

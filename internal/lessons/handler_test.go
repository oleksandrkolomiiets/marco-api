package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marco-api/internal/users"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubUserStore struct {
	user *users.User
	err  error
}

func (s *stubUserStore) CreateUser(_ context.Context, _ users.CreateUserParams) (*users.User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByGoogleID(_ context.Context, _ string) (*users.User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByEmail(_ context.Context, _ string) (*users.User, error) {
	return s.user, s.err
}

func (s *stubUserStore) GetUserByID(_ context.Context, _ uuid.UUID) (*users.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func (s *stubUserStore) UpdateUser(_ context.Context, _ uuid.UUID, _ users.UpdateUserParams) (*users.User, error) {
	return s.user, s.err
}

func (s *stubUserStore) UpdatePassword(_ context.Context, _ uuid.UUID, _ string) error {
	return s.err
}

var _ users.UserStore = (*stubUserStore)(nil)

type upsertCall struct {
	userID   uuid.UUID
	lessonID uuid.UUID
	status   string
}

type stubLessonStore struct {
	lessons        []*Lesson
	listErr        error
	lesson         *Lesson
	getErr         error
	progressMap    map[uuid.UUID]string
	progressMapErr error
	progress       *LessonProgress
	getProgressErr error
	upserted       *LessonProgress
	upsertErr      error

	gotLevel  string
	gotSlug   string
	gotUpsert *upsertCall
}

func (s *stubLessonStore) ListLessons(_ context.Context, level string) ([]*Lesson, error) {
	s.gotLevel = level
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.lessons, nil
}

func (s *stubLessonStore) GetLessonBySlug(_ context.Context, slug string) (*Lesson, error) {
	s.gotSlug = slug
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.lesson, nil
}

func (s *stubLessonStore) ListProgressByUserID(_ context.Context, _ uuid.UUID) (map[uuid.UUID]string, error) {
	if s.progressMapErr != nil {
		return nil, s.progressMapErr
	}
	return s.progressMap, nil
}

func (s *stubLessonStore) GetProgress(_ context.Context, _, _ uuid.UUID) (*LessonProgress, error) {
	if s.getProgressErr != nil {
		return nil, s.getProgressErr
	}
	return s.progress, nil
}

func (s *stubLessonStore) UpsertProgress(_ context.Context, userID, lessonID uuid.UUID, status string) (*LessonProgress, error) {
	s.gotUpsert = &upsertCall{userID: userID, lessonID: lessonID, status: status}
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	return s.upserted, nil
}

var _ LessonStore = (*stubLessonStore)(nil)

func registerLessonRoutes(app *fiber.App, h *Handler) {
	app.Get("/api/v1/lessons", h.ListLessons)
	app.Get("/api/v1/lessons/:slug", h.GetLesson)
	app.Put("/api/v1/lessons/:slug/progress", h.UpdateProgress)
}

func newLessonsApp(h *Handler, userID uuid.UUID) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	registerLessonRoutes(app, h)
	return app
}

func newLessonsAppNoAuth(h *Handler) *fiber.App {
	app := fiber.New()
	registerLessonRoutes(app, h)
	return app
}

func testAppUser(id uuid.UUID, plan string) *users.User {
	return &users.User{
		ID:    id,
		Email: "joost@example.com",
		Plan:  plan,
	}
}

func freeLesson() *Lesson {
	return &Lesson{
		ID:         uuid.New(),
		Slug:       "bandeja-basics",
		Title:      "Bandeja basics",
		Level:      "beginner",
		OrderIndex: 1,
		IsFree:     true,
		Published:  true,
		CuePoints:  []CuePoint{},
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func premiumLesson() *Lesson {
	return &Lesson{
		ID:         uuid.New(),
		Slug:       "vibora-advanced",
		Title:      "Vibora advanced",
		Level:      "advanced",
		OrderIndex: 7,
		IsFree:     false,
		Published:  true,
		CuePoints:  []CuePoint{},
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func errBody(t *testing.T, resp interface{ Read([]byte) (int, error) }) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp).Decode(&body))
	return body["error"]
}

func TestHandler_ListLessons_FreeUserLockingAndProgress(t *testing.T) {
	userID := uuid.New()
	free := freeLesson()
	premium := premiumLesson()
	lessonStore := &stubLessonStore{
		lessons:     []*Lesson{free, premium},
		progressMap: map[uuid.UUID]string{free.ID: "learned"},
	}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var views []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&views))
	require.Len(t, views, 2)

	// Free lesson: unlocked, carries the user's progress.
	assert.Equal(t, "bandeja-basics", views[0]["slug"])
	assert.Equal(t, false, views[0]["locked"])
	assert.Equal(t, "learned", views[0]["progress"])

	// Premium lesson on a free plan: locked, no progress recorded.
	assert.Equal(t, "vibora-advanced", views[1]["slug"])
	assert.Equal(t, true, views[1]["locked"])
	assert.Nil(t, views[1]["progress"])

	assert.Equal(t, "", lessonStore.gotLevel, "no level query means unfiltered list")
}

func TestHandler_ListLessons_PremiumUserNothingLocked(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{
		lessons:     []*Lesson{freeLesson(), premiumLesson()},
		progressMap: map[uuid.UUID]string{},
	}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "premium")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var views []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&views))
	require.Len(t, views, 2)
	assert.Equal(t, false, views[0]["locked"])
	assert.Equal(t, false, views[1]["locked"], "premium plan unlocks premium lessons")
}

func TestHandler_ListLessons_LevelFilter(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{
		lessons:     []*Lesson{freeLesson()},
		progressMap: map[uuid.UUID]string{},
	}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons?level=beginner", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "beginner", lessonStore.gotLevel, "level query must be passed to the store")
}

func TestHandler_ListLessons_InvalidLevel(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons?level=expert", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "level must be one of: beginner, intermediate, advanced", errBody(t, resp.Body))
}

func TestHandler_ListLessons_StoreErrors(t *testing.T) {
	tests := []struct {
		name  string
		store *stubLessonStore
	}{
		{"lessons query fails", &stubLessonStore{listErr: errors.New("db down")}},
		{"progress query fails", &stubLessonStore{lessons: []*Lesson{freeLesson()}, progressMapErr: errors.New("db down")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, tt.store)
			app := newLessonsApp(h, userID)

			req := httptest.NewRequest("GET", "/api/v1/lessons", nil)
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
			assert.Equal(t, "internal server error", errBody(t, resp.Body))
		})
	}
}

func TestHandler_Unauthorized(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"ListLessons", "GET", "/api/v1/lessons"},
		{"GetLesson", "GET", "/api/v1/lessons/bandeja-basics"},
		{"UpdateProgress", "PUT", "/api/v1/lessons/bandeja-basics/progress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&stubUserStore{}, &stubLessonStore{})
			app := newLessonsAppNoAuth(h)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"status":"viewed"}`))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
			assert.Equal(t, "unauthorized", errBody(t, resp.Body))
		})
	}
}

func TestHandler_UserLookupFailures(t *testing.T) {
	tests := []struct {
		name       string
		userErr    error
		wantStatus int
		wantErr    string
	}{
		{"user not found", pgx.ErrNoRows, fiber.StatusNotFound, "user not found"},
		{"user store error", errors.New("db down"), fiber.StatusInternalServerError, "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&stubUserStore{err: tt.userErr}, &stubLessonStore{})
			app := newLessonsApp(h, uuid.New())

			req := httptest.NewRequest("GET", "/api/v1/lessons", nil)
			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			assert.Equal(t, tt.wantErr, errBody(t, resp.Body))
		})
	}
}

func TestHandler_GetLesson_FoundWithProgress(t *testing.T) {
	userID := uuid.New()
	lesson := freeLesson()
	lessonStore := &stubLessonStore{
		lesson: lesson,
		progress: &LessonProgress{
			ID:       uuid.New(),
			UserID:   userID,
			LessonID: lesson.ID,
			Status:   "mastered",
		},
	}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons/bandeja-basics", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var detail map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	assert.Equal(t, "bandeja-basics", detail["slug"])
	assert.Equal(t, "Bandeja basics", detail["title"])
	assert.Equal(t, "mastered", detail["progress"])
	assert.Equal(t, "bandeja-basics", lessonStore.gotSlug)
}

func TestHandler_GetLesson_NoProgressYet(t *testing.T) {
	// GetProgress returning pgx.ErrNoRows is not an error — progress is just null.
	userID := uuid.New()
	lessonStore := &stubLessonStore{lesson: freeLesson(), getProgressErr: pgx.ErrNoRows}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons/bandeja-basics", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var detail map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	assert.Equal(t, "bandeja-basics", detail["slug"])
	assert.Nil(t, detail["progress"])
}

func TestHandler_GetLesson_NotFound(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{getErr: pgx.ErrNoRows}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons/does-not-exist", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "lesson not found", errBody(t, resp.Body))
}

func TestHandler_GetLesson_PremiumGatedForFreeUser(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{lesson: premiumLesson()}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons/vibora-advanced", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "upgrade_required", errBody(t, resp.Body))
}

func TestHandler_GetLesson_StoreError(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{getErr: errors.New("db down")}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("GET", "/api/v1/lessons/bandeja-basics", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal server error", errBody(t, resp.Body))
}

func TestHandler_UpdateProgress_ValidStatuses(t *testing.T) {
	for _, status := range []string{"viewed", "learned", "mastered"} {
		t.Run(status, func(t *testing.T) {
			userID := uuid.New()
			lesson := freeLesson()
			lessonStore := &stubLessonStore{
				lesson: lesson,
				upserted: &LessonProgress{
					ID:       uuid.New(),
					UserID:   userID,
					LessonID: lesson.ID,
					Status:   status,
				},
			}
			h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
			app := newLessonsApp(h, userID)

			req := httptest.NewRequest("PUT", "/api/v1/lessons/bandeja-basics/progress",
				strings.NewReader(`{"status":"`+status+`"}`))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusOK, resp.StatusCode)

			var got LessonProgress
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			assert.Equal(t, status, got.Status)
			assert.Equal(t, userID, got.UserID)
			assert.Equal(t, lesson.ID, got.LessonID)

			require.NotNil(t, lessonStore.gotUpsert)
			assert.Equal(t, userID, lessonStore.gotUpsert.userID)
			assert.Equal(t, lesson.ID, lessonStore.gotUpsert.lessonID)
			assert.Equal(t, status, lessonStore.gotUpsert.status)
		})
	}
}

func TestHandler_UpdateProgress_InvalidStatus(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"unknown status", `{"status":"completed"}`},
		{"empty status", `{"status":""}`},
		{"missing status", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			lessonStore := &stubLessonStore{lesson: freeLesson()}
			h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
			app := newLessonsApp(h, userID)

			req := httptest.NewRequest("PUT", "/api/v1/lessons/bandeja-basics/progress", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, fiber.StatusUnprocessableEntity, resp.StatusCode)
			assert.Equal(t, "status must be one of: viewed, learned, mastered", errBody(t, resp.Body))
			assert.Nil(t, lessonStore.gotUpsert, "store must not be called on invalid status")
		})
	}
}

func TestHandler_UpdateProgress_InvalidBody(t *testing.T) {
	userID := uuid.New()
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, &stubLessonStore{lesson: freeLesson()})
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("PUT", "/api/v1/lessons/bandeja-basics/progress", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "invalid request body", errBody(t, resp.Body))
}

func TestHandler_UpdateProgress_LessonNotFound(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{getErr: pgx.ErrNoRows}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("PUT", "/api/v1/lessons/does-not-exist/progress",
		strings.NewReader(`{"status":"viewed"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "lesson not found", errBody(t, resp.Body))
}

func TestHandler_UpdateProgress_PremiumGatedForFreeUser(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{lesson: premiumLesson()}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("PUT", "/api/v1/lessons/vibora-advanced/progress",
		strings.NewReader(`{"status":"viewed"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "upgrade_required", errBody(t, resp.Body))
	assert.Nil(t, lessonStore.gotUpsert, "store must not be called when gated")
}

func TestHandler_UpdateProgress_UpsertError(t *testing.T) {
	userID := uuid.New()
	lessonStore := &stubLessonStore{lesson: freeLesson(), upsertErr: errors.New("db down")}
	h := NewHandler(&stubUserStore{user: testAppUser(userID, "free")}, lessonStore)
	app := newLessonsApp(h, userID)

	req := httptest.NewRequest("PUT", "/api/v1/lessons/bandeja-basics/progress",
		strings.NewReader(`{"status":"viewed"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal server error", errBody(t, resp.Body))
}

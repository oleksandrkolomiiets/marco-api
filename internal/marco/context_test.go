package marco

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"marco-api/internal/anthropic"
)

const fixtureUserID = "11111111-1111-1111-1111-111111111111"

// truncateAll wipes every table the test seeds into. We do not truncate the
// migrations table — the schema stays intact between runs.
func truncateAll(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	const stmt = `
		TRUNCATE TABLE
			messages,
			match_logs,
			match_preparation,
			goals,
			user_lesson_progress,
			chat_messages,
			chat_sessions,
			refresh_tokens,
			lessons,
			users
		RESTART IDENTITY CASCADE
	`
	_, err := db.Exec(ctx, stmt)
	require.NoError(t, err, "truncate test DB")
}

func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect test DB")
	t.Cleanup(pool.Close)
	return pool
}

func execSQLFile(t *testing.T, ctx context.Context, db *pgxpool.Pool, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	_, err = db.Exec(ctx, string(b))
	require.NoError(t, err, "exec %s", path)
}

func TestAssembler_Build_GoldenFile(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	truncateAll(t, ctx, db)
	execSQLFile(t, ctx, db, filepath.Join("testdata", "seed.sql"))

	a := NewAssembler(db)
	uc, history, err := a.Build(ctx, uuid.MustParse(fixtureUserID))
	require.NoError(t, err)

	type golden struct {
		Context  UserContext         `json:"context"`
		Messages []anthropic.Message `json:"messages"`
	}
	got := golden{Context: uc, Messages: history}

	// `today` is time.Now()-derived; assert it separately and pin it so the
	// golden file stays stable across days.
	require.Equal(t, time.Now().Format("2006-01-02"), got.Context.Today)
	got.Context.Today = "2026-01-01"

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	goldenPath := filepath.Join("testdata", "golden_context.json")
	if _, err := os.Stat(goldenPath); os.IsNotExist(err) {
		require.NoError(t, os.WriteFile(goldenPath, append(gotJSON, '\n'), 0o644))
		t.Fatalf("golden file created at %s — review it and re-run the test to validate", goldenPath)
	}

	wantJSON, err := os.ReadFile(goldenPath)
	require.NoError(t, err)

	// Trim trailing newline for comparison stability.
	assert.JSONEq(t, string(wantJSON), string(gotJSON),
		"golden mismatch — actual output:\n%s", string(gotJSON))
}

func TestAssembler_Build_NewUser(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	truncateAll(t, ctx, db)

	const newUserID = "33333333-3333-3333-3333-333333333333"
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, plan)
		VALUES ($1, 'google-new', 'new@test.local', 'NewPlayer', 'beginner', 'right', 'right', '1x/week', 'free')
	`, newUserID)
	require.NoError(t, err)

	a := NewAssembler(db)
	uc, history, err := a.Build(ctx, uuid.MustParse(newUserID))
	require.NoError(t, err)

	assert.Equal(t, "NewPlayer", uc.User.Name)
	assert.NotNil(t, uc.Goals, "Goals should be empty slice, not nil")
	assert.Empty(t, uc.Goals)
	assert.NotNil(t, uc.Progress.Completed, "Progress.Completed should be empty slice, not nil")
	assert.Empty(t, uc.Progress.Completed)
	assert.NotNil(t, uc.Progress.Mastered, "Progress.Mastered should be empty slice, not nil")
	assert.Empty(t, uc.Progress.Mastered)
	assert.Empty(t, uc.Progress.InProgress)
	assert.Nil(t, uc.LastMatch, "LastMatch should be nil when no matches logged")
	assert.NotNil(t, history, "history should be empty slice, not nil")
	assert.Empty(t, history)
}

package chat

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"marco-api/internal/marco"
)

const storeFixtureUserID = "55555555-5555-5555-5555-555555555555"

func openStoreTestDB(t *testing.T) *pgxpool.Pool {
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

func resetMessagesAndSeedUser(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
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
			users
		RESTART IDENTITY CASCADE
	`
	_, err := db.Exec(ctx, stmt)
	require.NoError(t, err, "truncate test DB")

	_, err = db.Exec(ctx, `
		INSERT INTO users (id, google_id, email, display_name, skill_level, dominant_hand, court_side, play_frequency, plan)
		VALUES ($1, 'google-store-test', 'store@test.local', 'Storer', 'beginner', 'right', 'right', '2x/week', 'free')
	`, storeFixtureUserID)
	require.NoError(t, err, "seed fixture user")
}

func countMessages(t *testing.T, ctx context.Context, db *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE user_id = $1`, userID).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestStore_SaveTurn_PersistsBothMessages(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	s := NewStore(db)

	userMsgID, assistantID, err := s.SaveTurn(ctx, userID, "hello marco", "vamos!", nil)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, userMsgID)
	assert.NotEqual(t, uuid.Nil, assistantID)
	assert.NotEqual(t, userMsgID, assistantID)

	rows, err := db.Query(ctx, `
		SELECT role, content, lesson_refs
		FROM messages
		WHERE user_id = $1
		ORDER BY created_at ASC, role DESC
	`, userID)
	require.NoError(t, err)
	defer rows.Close()

	type row struct {
		Role       string
		Content    string
		LessonRefs []byte
	}
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.Role, &r.Content, &r.LessonRefs))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)

	// Find user and assistant rows by role since ordering with same created_at can vary.
	var userRow, assistantRow row
	for _, r := range got {
		if r.Role == "user" {
			userRow = r
		} else {
			assistantRow = r
		}
	}
	assert.Equal(t, "user", userRow.Role)
	assert.Equal(t, "hello marco", userRow.Content)
	assert.JSONEq(t, "[]", string(userRow.LessonRefs))

	assert.Equal(t, "assistant", assistantRow.Role)
	assert.Equal(t, "vamos!", assistantRow.Content)
	assert.JSONEq(t, "[]", string(assistantRow.LessonRefs))
}

func TestStore_SaveTurn_StoresLessonRefs(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	s := NewStore(db)

	refs := []marco.LessonRef{
		{ID: "bdj_001", Title: "Bandeja basics"},
		{ID: "net_pos_002", Title: "Net positioning fundamentals"},
	}

	_, assistantID, err := s.SaveTurn(ctx, userID, "what should I learn?",
		`Try [LESSON_REF: bdj_001 | "Bandeja basics"] then [LESSON_REF: net_pos_002 | "Net positioning fundamentals"].`,
		refs,
	)
	require.NoError(t, err)

	var raw []byte
	err = db.QueryRow(ctx, `SELECT lesson_refs FROM messages WHERE id = $1`, assistantID).Scan(&raw)
	require.NoError(t, err)

	var got []marco.LessonRef
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, refs, got)
}

func TestStore_SaveTurn_RejectsEmptyAssistantMessage(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	s := NewStore(db)

	_, _, err := s.SaveTurn(ctx, userID, "hi", "", nil)
	require.Error(t, err)
	assert.Equal(t, 0, countMessages(t, ctx, db, userID))
}

func TestStore_SaveTurn_RollsBackOnContextCancellation(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	s := NewStore(db)

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, _, err := s.SaveTurn(cancelCtx, userID, "hi", "response", nil)
	require.Error(t, err)
	assert.Equal(t, 0, countMessages(t, ctx, db, userID))
}

// seedTurns inserts n turns (user + assistant per turn) with distinct
// timestamps so cursor pagination has a stable order to walk through.
func seedTurns(t *testing.T, ctx context.Context, db *pgxpool.Pool, userID uuid.UUID, n int) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Duration(n) * time.Minute)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		_, err := db.Exec(ctx, `
			INSERT INTO messages (user_id, role, content, lesson_refs, created_at)
			VALUES ($1, 'user', $2, '[]'::jsonb, $3),
			       ($1, 'assistant', $4, '[]'::jsonb, $3)
		`, userID, "user-"+time.Duration(i).String(), ts, "assistant-"+time.Duration(i).String())
		require.NoError(t, err)
	}
}

func TestStore_GetHistory_DefaultLimitReturnsNewest30(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	seedTurns(t, ctx, db, userID, 25) // 50 messages, 25 turns

	s := NewStore(db)
	msgs, hasMore, err := s.GetHistory(ctx, userID, 0, nil)
	require.NoError(t, err)

	assert.Len(t, msgs, 30, "default limit should be 30")
	assert.True(t, hasMore, "20 more messages exist beyond the page")

	for i := 1; i < len(msgs); i++ {
		assert.False(t, msgs[i].CreatedAt.Before(msgs[i-1].CreatedAt),
			"messages must be in ASC chronological order")
	}
}

func TestStore_GetHistory_HasMoreFalseWhenAtRoot(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	seedTurns(t, ctx, db, userID, 10) // 20 messages

	s := NewStore(db)
	msgs, hasMore, err := s.GetHistory(ctx, userID, 30, nil)
	require.NoError(t, err)

	assert.Len(t, msgs, 20)
	assert.False(t, hasMore, "no older messages exist beyond a single page that fits everything")
}

func TestStore_GetHistory_BeforeCursorReturnsOlderPage(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	seedTurns(t, ctx, db, userID, 25) // 50 messages

	s := NewStore(db)

	page1, hasMore1, err := s.GetHistory(ctx, userID, 30, nil)
	require.NoError(t, err)
	require.Len(t, page1, 30)
	require.True(t, hasMore1)

	cursor := page1[0].CreatedAt
	page2, hasMore2, err := s.GetHistory(ctx, userID, 30, &cursor)
	require.NoError(t, err)

	assert.Len(t, page2, 20, "20 messages remain older than page1's oldest")
	assert.False(t, hasMore2, "second page exhausts history")

	// No overlap: every page2 message is older than every page1 message.
	for _, m := range page2 {
		assert.True(t, m.CreatedAt.Before(cursor),
			"page2 messages must predate the cursor")
	}
}

func TestStore_GetHistory_LimitIsClampedToMax(t *testing.T) {
	ctx := context.Background()
	db := openStoreTestDB(t)
	resetMessagesAndSeedUser(t, ctx, db)

	userID := uuid.MustParse(storeFixtureUserID)
	seedTurns(t, ctx, db, userID, 60) // 120 messages

	s := NewStore(db)
	msgs, _, err := s.GetHistory(ctx, userID, 9999, nil)
	require.NoError(t, err)

	assert.Len(t, msgs, maxHistoryLimit)
}

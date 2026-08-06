package exam

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// seedOneQuestion writes a single question with a right and a wrong option, so
// the masking assertions do not depend on the real curriculum being loaded.
func seedOneQuestion(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(ctx, `TRUNCATE exam_questions RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	var qid string
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO exam_questions (slug, order_index, category, prompt, explanation)
		VALUES ('masking-probe', 1, 'Service', 'Diagonal or not?', 'FIP · Rule 6.5 · the serve must travel diagonally.')
		RETURNING id`).Scan(&qid))

	_, err = db.Exec(ctx, `
		INSERT INTO exam_options (question_id, order_index, text, is_correct)
		VALUES ($1, 1, 'Fault — must be diagonal', TRUE),
		       ($1, 2, 'Good serve', FALSE)`, qid)
	require.NoError(t, err)
}

// The take-the-exam payload must not carry the answer key. is_correct was
// already masked deliberately; the explanation was not, and it gives the answer
// away just as plainly — "the serve must travel diagonally" leaves exactly one
// option standing. The results screen reads its copy off the attempt review, so
// nothing on the client needs it here.
func TestListQuestions_WithholdsTheAnswerKey(t *testing.T) {
	db := openStoreTestDB(t)
	ctx := context.Background()
	seedOneQuestion(t, ctx, db)
	store := NewStore(db)

	paper, err := store.ListQuestions(ctx)
	require.NoError(t, err)
	require.Len(t, paper, 1)
	require.Len(t, paper[0].Options, 2)

	assert.Nil(t, paper[0].Explanation,
		"the explanation names the rule the question turns on — sending it with the paper hands over the answer")
	for _, o := range paper[0].Options {
		assert.False(t, o.IsCorrect, "take-the-exam payload must never reveal correct options")
	}
}

// The other half of the contract: review still carries everything the results
// screen needs to explain a wrong answer.
func TestGetQuestionsForReview_KeepsTheAnswerKey(t *testing.T) {
	db := openStoreTestDB(t)
	ctx := context.Background()
	seedOneQuestion(t, ctx, db)
	store := NewStore(db)

	review, err := store.GetQuestionsForReview(ctx)
	require.NoError(t, err)
	require.Len(t, review, 1)
	require.Len(t, review[0].Options, 2)

	require.NotNil(t, review[0].Explanation)
	assert.Contains(t, *review[0].Explanation, "Rule 6.5")

	correct := 0
	for _, o := range review[0].Options {
		if o.IsCorrect {
			correct++
		}
	}
	assert.Equal(t, 1, correct, "review must mark exactly one option correct")
}

// Every question needs exactly one correct option, or grading has nothing to
// compare against. Runs over whatever the database actually holds, so a bad
// seed is caught rather than assumed away.
func TestSeededQuestions_HaveExactlyOneCorrectOption(t *testing.T) {
	db := openStoreTestDB(t)
	ctx := context.Background()

	rows, err := db.Query(ctx, `
		SELECT q.slug, count(*) FILTER (WHERE o.is_correct)
		FROM exam_questions q JOIN exam_options o ON o.question_id = q.id
		GROUP BY q.slug ORDER BY q.slug`)
	require.NoError(t, err)
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var slug string
		var correct int
		require.NoError(t, rows.Scan(&slug, &correct))
		assert.Equal(t, 1, correct, "question %q has %d correct options", slug, correct)
		seen++
	}
	require.NoError(t, rows.Err())
	if seen == 0 {
		t.Skip("no exam questions seeded in the test database")
	}
}

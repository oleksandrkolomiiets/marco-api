package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"marco-api/internal/marco"
)

const (
	defaultHistoryLimit = 30
	maxHistoryLimit     = 100
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// SaveTurn writes the user message and the assistant message in a single transaction.
// lessonRefs are stored on the assistant row only. Returns both message IDs so the
// caller can emit them in the SSE done event — the mobile client needs the assistant
// ID to set feedback and both IDs to soft-delete on retry.
func (s *Store) SaveTurn(
	ctx context.Context,
	userID uuid.UUID,
	userMessage string,
	assistantMessage string,
	lessonRefs []marco.LessonRef,
) (uuid.UUID, uuid.UUID, error) {
	if assistantMessage == "" {
		return uuid.Nil, uuid.Nil, errors.New("cannot save empty assistant response")
	}

	if lessonRefs == nil {
		lessonRefs = []marco.LessonRef{}
	}
	refsJSON, err := json.Marshal(lessonRefs)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("marshal lesson refs: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const insertUser = `
		INSERT INTO messages (user_id, role, content, lesson_refs)
		VALUES ($1, 'user', $2, '[]'::jsonb)
		RETURNING id
	`
	var userMsgID uuid.UUID
	if err := tx.QueryRow(ctx, insertUser, userID, userMessage).Scan(&userMsgID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert user message: %w", err)
	}

	const insertAssistant = `
		INSERT INTO messages (user_id, role, content, lesson_refs)
		VALUES ($1, 'assistant', $2, $3::jsonb)
		RETURNING id
	`
	var assistantID uuid.UUID
	if err := tx.QueryRow(ctx, insertAssistant, userID, assistantMessage, refsJSON).Scan(&assistantID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert assistant message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("commit tx: %w", err)
	}
	return userMsgID, assistantID, nil
}

// GetHistory returns one page of the user's messages in chronological (ASC)
// order, newest-page-first when paginating. If before is non-nil, only
// messages with created_at < *before are returned — this is the cursor the
// client passes to load older pages. limit is clamped to (0, maxHistoryLimit].
//
// hasMore is true when there are older messages beyond the returned page; the
// client uses it to stop paginating once the conversation root is reached.
//
// Assistant content has [LESSON_REF: ...] tokens stripped; refs are returned
// in their own field.
func (s *Store) GetHistory(ctx context.Context, userID uuid.UUID, limit int, before *time.Time) ([]Message, bool, error) {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	// Fetch one extra row so we can tell the client whether older messages exist
	// without a separate COUNT query.
	fetch := limit + 1

	var (
		rows pgx.Rows
		err  error
	)
	// EXISTS subquery lets us return logged-state in the same round-trip; the
	// partial index on match_logs(message_id) WHERE message_id IS NOT NULL keeps
	// it cheap on conversations where most messages have never spawned a match.
	// The match_preparation subquery uses the same shape so the chat UI can
	// render "Prep ready" across restarts (idx_match_preparation_message_id
	// keeps it cheap too).
	if before != nil {
		const q = `
			SELECT m.id, m.role, m.content, m.lesson_refs, m.feedback_score, m.created_at,
			       EXISTS(SELECT 1 FROM match_logs ml WHERE ml.message_id = m.id) AS match_logged,
			       (SELECT mp.id FROM match_preparation mp WHERE mp.message_id = m.id LIMIT 1) AS match_preparation_id
			FROM messages m
			WHERE m.user_id = $1 AND m.deleted_at IS NULL AND m.created_at < $2
			ORDER BY m.created_at DESC, CASE m.role WHEN 'assistant' THEN 0 ELSE 1 END ASC
			LIMIT $3
		`
		rows, err = s.db.Query(ctx, q, userID, *before, fetch)
	} else {
		const q = `
			SELECT m.id, m.role, m.content, m.lesson_refs, m.feedback_score, m.created_at,
			       EXISTS(SELECT 1 FROM match_logs ml WHERE ml.message_id = m.id) AS match_logged,
			       (SELECT mp.id FROM match_preparation mp WHERE mp.message_id = m.id LIMIT 1) AS match_preparation_id
			FROM messages m
			WHERE m.user_id = $1 AND m.deleted_at IS NULL
			ORDER BY m.created_at DESC, CASE m.role WHEN 'assistant' THEN 0 ELSE 1 END ASC
			LIMIT $2
		`
		rows, err = s.db.Query(ctx, q, userID, fetch)
	}
	if err != nil {
		return nil, false, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var msg Message
		var refsJSON json.RawMessage
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &refsJSON, &msg.FeedbackScore, &msg.CreatedAt, &msg.MatchLogged, &msg.MatchPreparationID); err != nil {
			return nil, false, fmt.Errorf("scan message: %w", err)
		}
		if err := json.Unmarshal(refsJSON, &msg.LessonRefs); err != nil {
			msg.LessonRefs = []marco.LessonRef{}
		}
		if msg.Role == "assistant" {
			// Parse the match-log / match-prep prefills from the raw content
			// BEFORE cleaning so the chat UI can render the "Log this match"
			// and "Adjust prep" tags long after the original stream — the SSE
			// match_log / match_prep events only fire once.
			msg.MatchLogPrefill = marco.ParseMatchLogToken(msg.Content)
			msg.MatchPrepPrefill = marco.ParseMatchPrepToken(msg.Content)
			msg.Content = marco.CleanContent(msg.Content)
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate messages: %w", err)
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	// Query is DESC for cursor pagination; flip to ASC so the response stays
	// chronological — the client appends each page directly without resorting.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	if msgs == nil {
		msgs = []Message{}
	}
	return msgs, hasMore, nil
}

// SetFeedback updates feedback_score on an assistant message owned by userID.
func (s *Store) SetFeedback(ctx context.Context, messageID uuid.UUID, userID uuid.UUID, score int8) error {
	const q = `
		UPDATE messages SET feedback_score = $1
		WHERE id = $2 AND user_id = $3 AND role = 'assistant' AND deleted_at IS NULL
	`
	tag, err := s.db.Exec(ctx, q, score, messageID, userID)
	if err != nil {
		return fmt.Errorf("set feedback: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// SoftDelete marks a message as deleted_at = NOW() so it no longer appears in
// GetHistory. Scoped to the authenticated user. Idempotent — re-deleting an
// already-deleted message returns pgx.ErrNoRows.
func (s *Store) SoftDelete(ctx context.Context, messageID uuid.UUID, userID uuid.UUID) error {
	const q = `
		UPDATE messages SET deleted_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`
	tag, err := s.db.Exec(ctx, q, messageID, userID)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

package exam

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	ListQuestions(ctx context.Context) ([]Question, error)
	GetQuestionsForReview(ctx context.Context) ([]Question, error)
	SubmitAttempt(ctx context.Context, userID uuid.UUID, picks map[uuid.UUID]uuid.UUID) (*AttemptReview, error)
	GetLatestAttempt(ctx context.Context, userID uuid.UUID) (*AttemptReview, error)
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return &pgxStore{pool: pool}
}

// listQuestionsInternal returns every question with options. If withCorrect
// is false, every option's IsCorrect is forced to false before returning.
func (s *pgxStore) listQuestionsInternal(ctx context.Context, withCorrect bool) ([]Question, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT q.id, q.slug, q.order_index, q.category, q.prompt, q.explanation,
		        o.id, o.order_index, o.text, o.is_correct
		 FROM exam_questions q
		 JOIN exam_options o ON o.question_id = q.id
		 ORDER BY q.order_index, o.order_index`,
	)
	if err != nil {
		return nil, fmt.Errorf("query exam questions: %w", err)
	}
	defer rows.Close()

	out := make([]Question, 0, 20)
	byID := make(map[uuid.UUID]*Question)
	for rows.Next() {
		var (
			q Question
			o Option
		)
		if err := rows.Scan(
			&q.ID, &q.Slug, &q.OrderIndex, &q.Category, &q.Prompt, &q.Explanation,
			&o.ID, &o.OrderIndex, &o.Text, &o.IsCorrect,
		); err != nil {
			return nil, fmt.Errorf("scan exam question: %w", err)
		}
		if !withCorrect {
			o.IsCorrect = false
		}
		existing, ok := byID[q.ID]
		if !ok {
			q.Options = []Option{o}
			out = append(out, q)
			byID[q.ID] = &out[len(out)-1]
		} else {
			existing.Options = append(existing.Options, o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exam questions: %w", err)
	}
	return out, nil
}

func (s *pgxStore) ListQuestions(ctx context.Context) ([]Question, error) {
	return s.listQuestionsInternal(ctx, false)
}

func (s *pgxStore) GetQuestionsForReview(ctx context.Context) ([]Question, error) {
	return s.listQuestionsInternal(ctx, true)
}

// SubmitAttempt grades the user's picks and persists the attempt. picks maps
// question_id → selected_option_id; missing questions are graded wrong with
// SelectedOptionID = nil.
func (s *pgxStore) SubmitAttempt(ctx context.Context, userID uuid.UUID, picks map[uuid.UUID]uuid.UUID) (*AttemptReview, error) {
	questions, err := s.GetQuestionsForReview(ctx)
	if err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, errors.New("exam has no questions seeded")
	}

	result, err := gradeAttempt(questions, picks)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var attempt Attempt
	if err := tx.QueryRow(ctx,
		`INSERT INTO exam_attempts (user_id, score, total, passed)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, user_id, score, total, passed, completed_at`,
		userID, result.score, result.total, result.passed,
	).Scan(&attempt.ID, &attempt.UserID, &attempt.Score, &attempt.Total, &attempt.Passed, &attempt.CompletedAt); err != nil {
		return nil, fmt.Errorf("insert attempt: %w", err)
	}

	batch := &pgx.Batch{}
	for _, ga := range result.answers {
		batch.Queue(
			`INSERT INTO exam_attempt_answers (attempt_id, question_id, selected_option_id, is_correct)
			 VALUES ($1, $2, $3, $4)`,
			attempt.ID, ga.questionID, ga.selectedOptionID, ga.isCorrect,
		)
	}
	br := tx.SendBatch(ctx, batch)
	for range result.answers {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return nil, fmt.Errorf("insert answer: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return nil, fmt.Errorf("close batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &AttemptReview{Attempt: attempt, Questions: questionReviews(questions, result.answers)}, nil
}

func (s *pgxStore) GetLatestAttempt(ctx context.Context, userID uuid.UUID) (*AttemptReview, error) {
	var attempt Attempt
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, score, total, passed, completed_at
		 FROM exam_attempts
		 WHERE user_id = $1
		 ORDER BY completed_at DESC
		 LIMIT 1`,
		userID,
	).Scan(&attempt.ID, &attempt.UserID, &attempt.Score, &attempt.Total, &attempt.Passed, &attempt.CompletedAt)
	if err != nil {
		return nil, err
	}

	questions, err := s.GetQuestionsForReview(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT question_id, selected_option_id, is_correct
		 FROM exam_attempt_answers
		 WHERE attempt_id = $1`,
		attempt.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("query answers: %w", err)
	}
	defer rows.Close()

	type pick struct {
		selected  *uuid.UUID
		isCorrect bool
	}
	picks := make(map[uuid.UUID]pick)
	for rows.Next() {
		var (
			qid uuid.UUID
			sid *uuid.UUID
			ok  bool
		)
		if err := rows.Scan(&qid, &sid, &ok); err != nil {
			return nil, fmt.Errorf("scan answer: %w", err)
		}
		picks[qid] = pick{selected: sid, isCorrect: ok}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate answers: %w", err)
	}

	review := &AttemptReview{Attempt: attempt, Questions: make([]QuestionReview, 0, len(questions))}
	for _, q := range questions {
		var correctID uuid.UUID
		for _, o := range q.Options {
			if o.IsCorrect {
				correctID = o.ID
				break
			}
		}
		p := picks[q.ID]
		review.Questions = append(review.Questions, QuestionReview{
			Question:         q,
			SelectedOptionID: p.selected,
			CorrectOptionID:  correctID,
			IsCorrect:        p.isCorrect,
		})
	}
	return review, nil
}

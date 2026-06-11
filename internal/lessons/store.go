package lessons

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LessonStore interface {
	ListLessons(ctx context.Context, level string) ([]*Lesson, error)
	GetLessonBySlug(ctx context.Context, slug string) (*Lesson, error)
	ListProgressByUserID(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error)
	GetProgress(ctx context.Context, userID, lessonID uuid.UUID) (*LessonProgress, error)
	UpsertProgress(ctx context.Context, userID, lessonID uuid.UUID, status string) (*LessonProgress, error)
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewLessonStore(pool *pgxpool.Pool) LessonStore {
	return &pgxStore{pool: pool}
}

// lessonSelect aggregates each lesson with its cues (ordered) and its single
// drill via correlated subqueries — keeps the result one row per lesson.
const lessonSelect = `
SELECT
  l.id, l.slug, l.title, l.level, l.order_index,
  l.tagline, l.focus,
  l.video_url, l.thumbnail_url, l.duration_seconds,
  l.common_mistake_pct, l.common_mistake_text,
  l.is_free, l.published, l.created_at,
  COALESCE(
    (SELECT json_agg(
              json_build_object(
                'timestamp_seconds', lc.timestamp_seconds,
                'cue_text', lc.cue_text
              ) ORDER BY lc.sort_order
            )
     FROM lesson_cues lc WHERE lc.lesson_id = l.id),
    '[]'::json
  ) AS cue_points,
  (SELECT json_build_object(
            'name', d.name,
            'duration_minutes', d.duration_minutes,
            'is_recommended', d.is_recommended,
            'description', d.description
          )
   FROM drills d WHERE d.lesson_id = l.id) AS drill
FROM lessons l
`

type scanner interface {
	Scan(dest ...any) error
}

func scanLesson(s scanner) (*Lesson, error) {
	var (
		l        Lesson
		cuesRaw  []byte
		drillRaw []byte
	)
	err := s.Scan(
		&l.ID,
		&l.Slug,
		&l.Title,
		&l.Level,
		&l.OrderIndex,
		&l.Tagline,
		&l.Focus,
		&l.VideoURL,
		&l.ThumbnailURL,
		&l.DurationSeconds,
		&l.CommonMistakePct,
		&l.CommonMistakeText,
		&l.IsFree,
		&l.Published,
		&l.CreatedAt,
		&cuesRaw,
		&drillRaw,
	)
	if err != nil {
		return nil, err
	}

	l.CuePoints = make([]CuePoint, 0)
	if len(cuesRaw) > 0 {
		if err := json.Unmarshal(cuesRaw, &l.CuePoints); err != nil {
			return nil, fmt.Errorf("decode cue_points: %w", err)
		}
	}
	if len(drillRaw) > 0 {
		var d Drill
		if err := json.Unmarshal(drillRaw, &d); err != nil {
			return nil, fmt.Errorf("decode drill: %w", err)
		}
		l.Drill = &d
	}

	return &l, nil
}

func scanProgress(s scanner) (*LessonProgress, error) {
	var p LessonProgress
	err := s.Scan(&p.ID, &p.UserID, &p.LessonID, &p.Status, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *pgxStore) ListLessons(ctx context.Context, level string) ([]*Lesson, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if level == "" {
		rows, err = s.pool.Query(ctx,
			lessonSelect+`
			WHERE l.published = true
			ORDER BY l.level, l.order_index`,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			lessonSelect+`
			WHERE l.published = true AND l.level = $1
			ORDER BY l.order_index`,
			level,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query lessons: %w", err)
	}
	defer rows.Close()

	lessons := make([]*Lesson, 0)
	for rows.Next() {
		l, err := scanLesson(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lesson: %w", err)
		}
		lessons = append(lessons, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lessons: %w", err)
	}
	return lessons, nil
}

func (s *pgxStore) GetLessonBySlug(ctx context.Context, slug string) (*Lesson, error) {
	row := s.pool.QueryRow(ctx,
		lessonSelect+` WHERE l.slug = $1 AND l.published = true`,
		slug,
	)
	return scanLesson(row)
}

func (s *pgxStore) ListProgressByUserID(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT lesson_id, status FROM user_lesson_progress WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query progress: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string)
	for rows.Next() {
		var lessonID uuid.UUID
		var status string
		if err := rows.Scan(&lessonID, &status); err != nil {
			return nil, fmt.Errorf("scan progress: %w", err)
		}
		out[lessonID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate progress: %w", err)
	}
	return out, nil
}

func (s *pgxStore) GetProgress(ctx context.Context, userID, lessonID uuid.UUID) (*LessonProgress, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, lesson_id, status, updated_at
		 FROM user_lesson_progress
		 WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID,
	)
	return scanProgress(row)
}

func (s *pgxStore) UpsertProgress(ctx context.Context, userID, lessonID uuid.UUID, status string) (*LessonProgress, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO user_lesson_progress (user_id, lesson_id, status)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, lesson_id) DO UPDATE SET status = EXCLUDED.status
		 RETURNING id, user_id, lesson_id, status, updated_at`,
		userID, lessonID, status,
	)
	return scanProgress(row)
}

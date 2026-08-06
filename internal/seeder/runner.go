package seeder

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FreeLessonMax is the highest lesson_number that should be seeded with
// is_free = true. Lessons 1..FreeLessonMax are free; the rest are premium.
const FreeLessonMax = 3

// Seed performs a single-transaction wipe + reinsert of every lesson, cue, and
// drill row. The truncate cascades through user_lesson_progress because lessons
// are the parent of that table; child tables (lesson_cues, drills) are listed
// explicitly so the truncate order is obvious from the code.
func Seed(ctx context.Context, pool *pgxpool.Pool, lessons []Lesson) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Children first, then the parent. user_lesson_progress is wiped too —
	// FKs to lessons mean any seed that changes lesson identity must reset
	// user progress to avoid orphaned rows.
	if _, err := tx.Exec(ctx,
		`TRUNCATE TABLE lesson_cues, drills, user_lesson_progress, lessons RESTART IDENTITY`,
	); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	for _, l := range lessons {
		log.Printf("seeding lesson %d/%d: %s", l.Number, len(lessons), l.Title)

		slug := Slugify(l.Title)
		isFree := l.Number <= FreeLessonMax

		var lessonID string
		err := tx.QueryRow(ctx, `
			INSERT INTO lessons (
				slug, title, level, order_index,
				tagline, focus,
				common_mistake_text,
				is_free, published
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id
		`,
			slug, l.Title, l.Level, l.Number,
			l.Tagline, l.Focus,
			l.Mistake.Text,
			isFree, true,
		).Scan(&lessonID)
		if err != nil {
			return fmt.Errorf("insert lesson %d (%q): %w", l.Number, l.Title, err)
		}

		for i, cue := range l.Cues {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lesson_cues (lesson_id, timestamp_seconds, cue_text, sort_order)
				VALUES ($1, $2, $3, $4)
			`,
				lessonID, cue.TimestampSeconds, cue.CueText, i,
			); err != nil {
				return fmt.Errorf("insert cue for lesson %d at %ds: %w", l.Number, cue.TimestampSeconds, err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO drills (lesson_id, name, duration_minutes, is_recommended, description)
			VALUES ($1, $2, $3, $4, $5)
		`,
			lessonID, l.Drill.Name, l.Drill.DurationMinutes, l.Drill.IsRecommended, l.Drill.Description,
		); err != nil {
			return fmt.Errorf("insert drill for lesson %d: %w", l.Number, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	log.Printf("seeded %d lessons", len(lessons))
	return nil
}

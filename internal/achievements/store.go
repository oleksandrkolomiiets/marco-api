package achievements

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

type Store interface {
	GetForUser(ctx context.Context, userID uuid.UUID) (*Summary, error)
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return &pgxStore{pool: pool}
}

func (s *pgxStore) GetForUser(ctx context.Context, userID uuid.UUID) (*Summary, error) {
	stats, err := s.gather(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("gather achievement stats: %w", err)
	}
	summary := compute(*stats)
	return &summary, nil
}

func (s *pgxStore) gather(ctx context.Context, userID uuid.UUID) (*userStats, error) {
	out := &userStats{}

	// The four stat groups are independent reads writing disjoint fields of
	// out — run them concurrently (g.Wait gives the caller the happens-before).
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.fillLessonStats(gctx, userID, out) })
	g.Go(func() error { return s.fillMatchStats(gctx, userID, out) })
	g.Go(func() error { return s.fillExamStats(gctx, userID, out) })
	g.Go(func() error { return s.fillMessageStats(gctx, userID, out) })
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if out.PublishedLessonCount > 0 && out.MasteredCount*2 >= out.PublishedLessonCount {
		// Approximate the unlock time as the most recent mastery — exact
		// crossing is rarely worth a second query.
		out.HalfMasteryAt = out.lastMasteryAt
	}
	return out, nil
}

// userStats here gains one extra working field that isn't serialised.
// Keep it un-exported by shadowing in a tiny helper struct.

func (s *pgxStore) fillLessonStats(ctx context.Context, userID uuid.UUID, out *userStats) error {
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lessons WHERE published = true`,
	).Scan(&out.PublishedLessonCount); err != nil {
		return fmt.Errorf("count lessons: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT l.slug, p.status, p.updated_at
		   FROM user_lesson_progress p
		   JOIN lessons l ON l.id = p.lesson_id
		  WHERE p.user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("query progress: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var slug, status string
		var updatedAt time.Time
		if err := rows.Scan(&slug, &status, &updatedAt); err != nil {
			return fmt.Errorf("scan progress: %w", err)
		}
		learned := status == "learned" || status == "mastered"
		mastered := status == "mastered"

		if learned {
			out.LearnedCount++
			if !out.HasLearnedLesson || earlier(out.FirstLessonAt, updatedAt) {
				out.FirstLessonAt = clonePtr(updatedAt)
			}
			out.HasLearnedLesson = true
		}
		if mastered {
			out.MasteredCount++
			if out.lastMasteryAt == nil || updatedAt.After(*out.lastMasteryAt) {
				out.lastMasteryAt = clonePtr(updatedAt)
			}
		}

		if strings.Contains(slug, "bandeja") {
			out.BandejaStatus = highestStatus(out.BandejaStatus, status)
			if learned && (out.BandejaApprenticeAt == nil || earlier(out.BandejaApprenticeAt, updatedAt)) {
				out.BandejaApprenticeAt = clonePtr(updatedAt)
			}
		}
		if strings.Contains(slug, "vibora") || strings.Contains(slug, "víbora") {
			out.ViboraStatus = highestStatus(out.ViboraStatus, status)
			if mastered && (out.ViboraMasteredAt == nil || earlier(out.ViboraMasteredAt, updatedAt)) {
				out.ViboraMasteredAt = clonePtr(updatedAt)
			}
		}
	}
	return rows.Err()
}

// highestStatus returns the further-along of two statuses on the
// viewed → learned → mastered ladder. Used when a lesson might somehow appear
// with multiple rows, and to default an empty starting status.
func highestStatus(existing, candidate string) string {
	rank := map[string]int{"": 0, "viewed": 1, "learned": 2, "mastered": 3}
	if rank[candidate] > rank[existing] {
		return candidate
	}
	return existing
}

func (s *pgxStore) fillMatchStats(ctx context.Context, userID uuid.UUID, out *userStats) error {
	rows, err := s.pool.Query(ctx,
		`SELECT played, result, created_at
		   FROM match_logs
		  WHERE user_id = $1
		  ORDER BY played_on ASC, created_at ASC`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("query match logs: %w", err)
	}
	defer rows.Close()

	streak := 0
	lossesBefore := 0
	for rows.Next() {
		var played bool
		var result *string
		var createdAt time.Time
		if err := rows.Scan(&played, &result, &createdAt); err != nil {
			return fmt.Errorf("scan match log: %w", err)
		}
		out.MatchCount++
		if out.MatchCount == 10 {
			out.MatchDiaristAt = clonePtr(createdAt)
		}

		if !played || result == nil {
			continue
		}
		switch *result {
		case "won":
			out.WinCount++
			if out.FirstWinAt == nil {
				out.FirstWinAt = clonePtr(createdAt)
			}
			streak++
			if streak > out.LongestWinStreak {
				out.LongestWinStreak = streak
			}
			if streak == 3 && out.WinStreakThreeAt == nil {
				out.WinStreakThreeAt = clonePtr(createdAt)
			}
			if lossesBefore >= 2 && !out.HadComeback {
				out.HadComeback = true
				out.ComebackAt = clonePtr(createdAt)
			}
			lossesBefore = 0
		case "lost":
			streak = 0
			lossesBefore++
		default:
			// draw / unknown — neither extends nor breaks the comeback setup
			streak = 0
		}
	}
	return rows.Err()
}

func (s *pgxStore) fillExamStats(ctx context.Context, userID uuid.UUID, out *userStats) error {
	// Latest attempt — drives the "Your progress" line whether or not it passed.
	var score, total int
	err := s.pool.QueryRow(ctx,
		`SELECT score, total
		   FROM exam_attempts
		  WHERE user_id = $1
		  ORDER BY completed_at DESC
		  LIMIT 1`,
		userID,
	).Scan(&score, &total)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("query latest exam: %w", err)
	}
	if err == nil {
		out.HasExamAttempt = true
		out.LatestExamScore = score
		out.LatestExamTotal = total
	}

	// First passed attempt — sets unlock time on the padel-license badge.
	var passedAt time.Time
	err = s.pool.QueryRow(ctx,
		`SELECT completed_at
		   FROM exam_attempts
		  WHERE user_id = $1 AND passed = true
		  ORDER BY completed_at ASC
		  LIMIT 1`,
		userID,
	).Scan(&passedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("query passed exam: %w", err)
	}
	out.PassedExam = true
	out.ExamPassedAt = clonePtr(passedAt)
	return nil
}

func (s *pgxStore) fillMessageStats(ctx context.Context, userID uuid.UUID, out *userStats) error {
	var count int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM messages WHERE user_id = $1 AND role = 'user' AND deleted_at IS NULL`,
		userID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count messages: %w", err)
	}
	out.UserMessageCount = count

	if count >= 10 {
		var at time.Time
		err := s.pool.QueryRow(ctx,
			`SELECT created_at FROM messages
			  WHERE user_id = $1 AND role = 'user' AND deleted_at IS NULL
			  ORDER BY created_at ASC OFFSET 9 LIMIT 1`,
			userID,
		).Scan(&at)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("query 10th message: %w", err)
		}
		if err == nil {
			out.MarcosCuriousAt = clonePtr(at)
		}
	}
	return nil
}

func earlier(existing *time.Time, candidate time.Time) bool {
	return existing == nil || candidate.Before(*existing)
}

func clonePtr(t time.Time) *time.Time {
	v := t
	return &v
}

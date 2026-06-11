package lessons

import (
	"time"

	"github.com/google/uuid"
)

type CuePoint struct {
	TimestampSeconds int    `json:"timestamp_seconds"`
	CueText          string `json:"cue_text"`
}

type Drill struct {
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes"`
	IsRecommended   bool   `json:"is_recommended"`
	Description     string `json:"description"`
}

type Lesson struct {
	ID                uuid.UUID  `json:"id"`
	Slug              string     `json:"slug"`
	Title             string     `json:"title"`
	Level             string     `json:"level"`
	OrderIndex        int        `json:"order_index"`
	Tagline           *string    `json:"tagline"`
	Focus             *string    `json:"focus"`
	VideoURL          *string    `json:"video_url"`
	ThumbnailURL      *string    `json:"thumbnail_url"`
	DurationSeconds   *int       `json:"duration_seconds"`
	CuePoints         []CuePoint `json:"cue_points"`
	CommonMistakePct  *int       `json:"common_mistake_pct"`
	CommonMistakeText *string    `json:"common_mistake_text"`
	Drill             *Drill     `json:"drill"`
	IsFree            bool       `json:"is_free"`
	Published         bool       `json:"published"`
	CreatedAt         time.Time  `json:"created_at"`
}

type LessonProgress struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	LessonID  uuid.UUID `json:"lesson_id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LessonView is the list-response shape: a lesson plus access and progress info.
type LessonView struct {
	*Lesson
	Locked   bool    `json:"locked"`
	Progress *string `json:"progress"`
}

// LessonDetail is the detail-response shape: a lesson plus the user's progress.
type LessonDetail struct {
	*Lesson
	Progress *string `json:"progress"`
}

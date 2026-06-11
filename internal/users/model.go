package users

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID `json:"id"`
	GoogleID      *string   `json:"-"`
	PasswordHash  *string   `json:"-"`
	Email         string    `json:"email"`
	DisplayName   *string   `json:"display_name"`
	AvatarURL     *string   `json:"avatar_url"`
	SkillLevel    *string   `json:"skill_level"`
	DominantHand  *string   `json:"dominant_hand"`
	CourtSide     *string   `json:"court_side"`
	PlayFrequency *string   `json:"play_frequency"`
	Goal          *string   `json:"goal"`
	Plan          string    `json:"plan"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateUserParams struct {
	GoogleID      *string
	PasswordHash  *string
	Email         string
	DisplayName   *string
	AvatarURL     *string
	SkillLevel    *string
	DominantHand  *string
	CourtSide     *string
	PlayFrequency *string
	Goal          *string
	Plan          *string // nil uses DB default 'free'
}

type UpdateUserParams struct {
	DisplayName   *string
	AvatarURL     *string
	SkillLevel    *string
	DominantHand  *string
	CourtSide     *string
	PlayFrequency *string
	Goal          *string
	Plan          *string
}

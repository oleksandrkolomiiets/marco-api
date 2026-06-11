package logs

import (
	"time"

	"github.com/google/uuid"
)

type MatchLog struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Played      bool      `json:"played"`
	Result      *string   `json:"result"`
	Feeling     *string   `json:"feeling"`
	Note        *string   `json:"note"`
	PartnerName *string   `json:"partner_name"`
	Opponents   []string  `json:"opponents"`
	PlayedOn    time.Time `json:"played_on"`
	CreatedAt   time.Time `json:"created_at"`
}

type PartnerSuggestion struct {
	PartnerName string `json:"partner_name"`
	MatchCount  int    `json:"match_count"`
}

package logs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MatchStore struct {
	db *pgxpool.Pool
}

func NewMatchStore(db *pgxpool.Pool) *MatchStore {
	return &MatchStore{db: db}
}

type CreateMatchParams struct {
	UserID      uuid.UUID
	Result      *string
	Feeling     *string
	Note        *string
	PartnerName *string
	Opponents   []string
	PlayedOn    time.Time
	// MessageID, when set, links this match log to the assistant chat message
	// that prompted it — used by the chat UI to mark the "Log this match" tag
	// as logged. Nil for matches created outside chat (e.g. Profile page).
	MessageID *uuid.UUID
}

func (s *MatchStore) CreateMatch(ctx context.Context, p CreateMatchParams) (MatchLog, error) {
	const q = `
		INSERT INTO match_logs (user_id, played, result, feeling, note, partner_name, opponents, played_on, message_id)
		VALUES ($1, true, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, played, result, feeling, note, partner_name, opponents, played_on, created_at
	`
	opponents := p.Opponents
	if opponents == nil {
		opponents = []string{}
	}
	var m MatchLog
	row := s.db.QueryRow(ctx, q,
		p.UserID, p.Result, p.Feeling, p.Note, p.PartnerName, opponents, p.PlayedOn, p.MessageID,
	)
	if err := row.Scan(
		&m.ID, &m.UserID, &m.Played, &m.Result, &m.Feeling,
		&m.Note, &m.PartnerName, &m.Opponents, &m.PlayedOn, &m.CreatedAt,
	); err != nil {
		return MatchLog{}, fmt.Errorf("create match: %w", err)
	}
	return m, nil
}

type UpdateMatchParams struct {
	UserID      uuid.UUID
	MatchID     uuid.UUID
	Result      *string
	Feeling     *string
	Note        *string
	PartnerName *string
	Opponents   []string
	PlayedOn    time.Time
}

var ErrMatchNotFound = fmt.Errorf("match not found")

func (s *MatchStore) UpdateMatch(ctx context.Context, p UpdateMatchParams) (MatchLog, error) {
	const q = `
		UPDATE match_logs
		SET result = $3, feeling = $4, note = $5, partner_name = $6, opponents = $7, played_on = $8
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, played, result, feeling, note, partner_name, opponents, played_on, created_at
	`
	opponents := p.Opponents
	if opponents == nil {
		opponents = []string{}
	}
	var m MatchLog
	row := s.db.QueryRow(ctx, q,
		p.MatchID, p.UserID, p.Result, p.Feeling, p.Note, p.PartnerName, opponents, p.PlayedOn,
	)
	if err := row.Scan(
		&m.ID, &m.UserID, &m.Played, &m.Result, &m.Feeling,
		&m.Note, &m.PartnerName, &m.Opponents, &m.PlayedOn, &m.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return MatchLog{}, ErrMatchNotFound
		}
		return MatchLog{}, fmt.Errorf("update match: %w", err)
	}
	return m, nil
}

func (s *MatchStore) ListMatches(ctx context.Context, userID uuid.UUID) ([]MatchLog, error) {
	const q = `
		SELECT id, user_id, played, result, feeling, note, partner_name, opponents, played_on, created_at
		FROM match_logs
		WHERE user_id = $1 AND played = true
		ORDER BY played_on DESC, created_at DESC
	`
	rows, err := s.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	defer rows.Close()

	out := []MatchLog{}
	for rows.Next() {
		var m MatchLog
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Played, &m.Result, &m.Feeling,
			&m.Note, &m.PartnerName, &m.Opponents, &m.PlayedOn, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matches: %w", err)
	}
	return out, nil
}

func (s *MatchStore) ListPartners(ctx context.Context, userID uuid.UUID) ([]PartnerSuggestion, error) {
	// Filter out empty/whitespace partner_names — older rows could be saved
	// blank by Marco's prefill, and they shouldn't appear as suggestions.
	const q = `
		SELECT partner_name, COUNT(*) AS match_count
		FROM match_logs
		WHERE user_id = $1 AND partner_name IS NOT NULL AND TRIM(partner_name) <> ''
		GROUP BY partner_name
		ORDER BY match_count DESC
		LIMIT 10
	`
	rows, err := s.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list partners: %w", err)
	}
	defer rows.Close()

	var out []PartnerSuggestion
	for rows.Next() {
		var p PartnerSuggestion
		if err := rows.Scan(&p.PartnerName, &p.MatchCount); err != nil {
			return nil, fmt.Errorf("scan partner: %w", err)
		}
		out = append(out, p)
	}
	if out == nil {
		out = []PartnerSuggestion{}
	}
	return out, nil
}

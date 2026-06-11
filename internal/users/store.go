package users

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserStore interface {
	CreateUser(ctx context.Context, params CreateUserParams) (*User, error)
	GetUserByGoogleID(ctx context.Context, googleID string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, params UpdateUserParams) (*User, error)
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &pgxStore{pool: pool}
}

const selectCols = `
	id, google_id, email, display_name, avatar_url, skill_level, dominant_hand,
	court_side, play_frequency, goal, plan, created_at, updated_at, password_hash`

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanUser(s scanner) (*User, error) {
	var u User
	err := s.Scan(
		&u.ID,
		&u.GoogleID,
		&u.Email,
		&u.DisplayName,
		&u.AvatarURL,
		&u.SkillLevel,
		&u.DominantHand,
		&u.CourtSide,
		&u.PlayFrequency,
		&u.Goal,
		&u.Plan,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.PasswordHash,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *pgxStore) CreateUser(ctx context.Context, p CreateUserParams) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users
			(google_id, password_hash, email, display_name, avatar_url, skill_level, dominant_hand, court_side, play_frequency, goal, plan)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, 'free'))
		RETURNING `+selectCols,
		p.GoogleID, p.PasswordHash, p.Email, p.DisplayName, p.AvatarURL, p.SkillLevel, p.DominantHand,
		p.CourtSide, p.PlayFrequency, p.Goal, p.Plan,
	)
	return scanUser(row)
}

func (s *pgxStore) GetUserByGoogleID(ctx context.Context, googleID string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+selectCols+` FROM users WHERE google_id = $1`,
		googleID,
	)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, pgx.ErrNoRows
	}
	return u, err
}

func (s *pgxStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+selectCols+` FROM users WHERE email = $1`,
		email,
	)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, pgx.ErrNoRows
	}
	return u, err
}

func (s *pgxStore) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+selectCols+` FROM users WHERE id = $1`,
		id,
	)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, pgx.ErrNoRows
	}
	return u, err
}

func (s *pgxStore) UpdateUser(ctx context.Context, id uuid.UUID, p UpdateUserParams) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE users SET
			display_name   = COALESCE($1, display_name),
			avatar_url     = COALESCE($2, avatar_url),
			skill_level    = COALESCE($3, skill_level),
			dominant_hand  = COALESCE($4, dominant_hand),
			court_side     = COALESCE($5, court_side),
			play_frequency = COALESCE($6, play_frequency),
			goal           = COALESCE($7, goal),
			plan           = COALESCE($8, plan)
		WHERE id = $9
		RETURNING`+selectCols,
		p.DisplayName, p.AvatarURL, p.SkillLevel, p.DominantHand, p.CourtSide,
		p.PlayFrequency, p.Goal, p.Plan, id,
	)
	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, pgx.ErrNoRows
	}
	return u, err
}

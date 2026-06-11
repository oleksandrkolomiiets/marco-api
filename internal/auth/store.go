package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
}

type AuthStore interface {
	SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	DeleteAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewAuthStore(pool *pgxpool.Pool) AuthStore {
	return &pgxStore{pool: pool}
}

func (s *pgxStore) SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	// Every sign-in/refresh inserts a row and nothing else removes expired
	// ones, so the table would grow without bound. Purge this user's expired
	// tokens opportunistically in the same round trip as the insert.
	_, err := s.pool.Exec(ctx, `
		WITH purged AS (
			DELETE FROM refresh_tokens WHERE user_id = $1 AND expires_at < NOW()
		)
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	return err
}

func (s *pgxStore) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	)
	var t RefreshToken
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return &t, nil
}

func (s *pgxStore) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *pgxStore) DeleteAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID)
	return err
}

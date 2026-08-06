package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	SessionID uuid.UUID
	TokenHash string
	ExpiresAt time.Time
}

// PasswordResetToken is one issued reset code. CodeHash is bcrypt, so it can
// only be verified against a candidate, never looked up by.
type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CodeHash  string
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
}

type AuthStore interface {
	// SessionStore is embedded rather than injected separately: sessions and
	// refresh tokens are written together on every sign-in and rotation, and
	// splitting them would mean two stubs to keep consistent in tests.
	SessionStore

	SaveRefreshToken(ctx context.Context, userID, sessionID uuid.UUID, tokenHash string, expiresAt time.Time) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	DeleteAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error

	// CreatePasswordResetToken supersedes any live code this user already has,
	// so only the newest one in their inbox works.
	CreatePasswordResetToken(ctx context.Context, userID uuid.UUID, codeHash string, expiresAt time.Time) error
	// GetLivePasswordResetToken returns the newest unconsumed, unexpired token
	// for a user, or pgx.ErrNoRows.
	GetLivePasswordResetToken(ctx context.Context, userID uuid.UUID) (*PasswordResetToken, error)
	// LastPasswordResetSentAt reports when this user was last issued a code, so
	// the handler can refuse to mail them again immediately. Zero time if never.
	LastPasswordResetSentAt(ctx context.Context, userID uuid.UUID) (time.Time, error)
	// IncrementPasswordResetAttempts records a wrong guess and returns the new
	// count, so the caller can burn the token once it's been guessed at enough.
	IncrementPasswordResetAttempts(ctx context.Context, id uuid.UUID) (int, error)
	// ConsumePasswordResetToken marks a token spent. It only affects a row that
	// is still unconsumed, so two racing resets can't both win.
	ConsumePasswordResetToken(ctx context.Context, id uuid.UUID) (bool, error)
	// ExpirePasswordResetTokens kills every live code for a user — used after a
	// successful reset and when a code has been guessed at too many times.
	ExpirePasswordResetTokens(ctx context.Context, userID uuid.UUID) error
}

type pgxStore struct {
	pool *pgxpool.Pool
}

func NewAuthStore(pool *pgxpool.Pool) AuthStore {
	return &pgxStore{pool: pool}
}

func (s *pgxStore) SaveRefreshToken(
	ctx context.Context, userID, sessionID uuid.UUID, tokenHash string, expiresAt time.Time,
) error {
	// Every sign-in/refresh inserts a row and nothing else removes expired
	// ones, so the table would grow without bound. Purge this user's expired
	// tokens opportunistically in the same round trip as the insert.
	_, err := s.pool.Exec(ctx, `
		WITH purged AS (
			DELETE FROM refresh_tokens WHERE user_id = $1 AND expires_at < NOW()
		)
		INSERT INTO refresh_tokens (user_id, session_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`,
		userID, sessionID, tokenHash, expiresAt,
	)
	return err
}

func (s *pgxStore) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, session_id, token_hash, expires_at
		   FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	)
	var t RefreshToken
	if err := row.Scan(&t.ID, &t.UserID, &t.SessionID, &t.TokenHash, &t.ExpiresAt); err != nil {
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

func (s *pgxStore) CreatePasswordResetToken(
	ctx context.Context, userID uuid.UUID, codeHash string, expiresAt time.Time,
) error {
	// Requesting a new code invalidates the old one in the same round trip.
	// Without this, every code a user ever requested would stay live until it
	// aged out, so an old email still worked after they asked for a fresh one.
	// Rows older than a day go too, so the table doesn't grow without bound —
	// nothing else ever deletes from it.
	_, err := s.pool.Exec(ctx, `
		WITH superseded AS (
			UPDATE password_reset_tokens
			   SET consumed_at = NOW()
			 WHERE user_id = $1 AND consumed_at IS NULL
		), purged AS (
			DELETE FROM password_reset_tokens
			 WHERE user_id = $1 AND created_at < NOW() - INTERVAL '1 day'
		)
		INSERT INTO password_reset_tokens (user_id, code_hash, expires_at)
		VALUES ($1, $2, $3)`,
		userID, codeHash, expiresAt,
	)
	return err
}

func (s *pgxStore) GetLivePasswordResetToken(
	ctx context.Context, userID uuid.UUID,
) (*PasswordResetToken, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, code_hash, expires_at, attempts, created_at
		  FROM password_reset_tokens
		 WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > NOW()
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userID,
	)
	var t PasswordResetToken
	if err := row.Scan(
		&t.ID, &t.UserID, &t.CodeHash, &t.ExpiresAt, &t.Attempts, &t.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *pgxStore) LastPasswordResetSentAt(
	ctx context.Context, userID uuid.UUID,
) (time.Time, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT created_at FROM password_reset_tokens
		 WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`,
		userID,
	)
	var at time.Time
	if err := row.Scan(&at); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return at, nil
}

func (s *pgxStore) IncrementPasswordResetAttempts(
	ctx context.Context, id uuid.UUID,
) (int, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE password_reset_tokens SET attempts = attempts + 1
		 WHERE id = $1 RETURNING attempts`,
		id,
	)
	var attempts int
	if err := row.Scan(&attempts); err != nil {
		return 0, err
	}
	return attempts, nil
}

func (s *pgxStore) ConsumePasswordResetToken(ctx context.Context, id uuid.UUID) (bool, error) {
	// The consumed_at IS NULL guard is what makes a code single-use under
	// concurrency: two requests carrying the same valid code both pass the
	// bcrypt check, but only one UPDATE matches a row.
	tag, err := s.pool.Exec(ctx, `
		UPDATE password_reset_tokens SET consumed_at = NOW()
		 WHERE id = $1 AND consumed_at IS NULL`,
		id,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *pgxStore) ExpirePasswordResetTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE password_reset_tokens SET consumed_at = NOW()
		 WHERE user_id = $1 AND consumed_at IS NULL`,
		userID,
	)
	return err
}

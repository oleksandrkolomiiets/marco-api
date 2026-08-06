package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Session is one signed-in device. It outlives the refresh tokens bound to it:
// /auth/refresh rotates the token but keeps the session, which is what lets
// the devices list show "signed in three weeks ago" instead of resetting the
// clock every fifteen minutes.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DeviceName *string
	Platform   *string
	AppVersion *string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// DeviceInfo is what a client tells us about itself, from request headers.
// Every field is optional.
type DeviceInfo struct {
	DeviceName string
	Platform   string
	AppVersion string
}

// SessionChecker is the one question the auth middleware asks on every
// authenticated request. Kept separate from SessionStore so the middleware
// depends on a single method rather than the whole session surface.
type SessionChecker interface {
	// IsSessionLive reports whether a session exists and has not been revoked.
	IsSessionLive(ctx context.Context, id uuid.UUID) (bool, error)
}

type SessionStore interface {
	SessionChecker

	// CreateSession starts a device session at sign-in.
	CreateSession(ctx context.Context, userID uuid.UUID, info DeviceInfo) (*Session, error)
	// ListSessions returns a user's live sessions, most recently seen first.
	ListSessions(ctx context.Context, userID uuid.UUID) ([]Session, error)
	// TouchSession bumps last_seen_at and refreshes the device details, which
	// change when the player updates the app or renames their phone. Returns
	// false if the session is gone or revoked, so a rotation can't resurrect
	// a device that was signed out from somewhere else.
	TouchSession(ctx context.Context, id uuid.UUID, info DeviceInfo) (bool, error)
	// RevokeSession signs one device out. Scoped by user so an id from
	// somewhere else can't be used to sign out a stranger.
	RevokeSession(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// RevokeOtherSessions signs out every device except the one asking.
	RevokeOtherSessions(ctx context.Context, userID, keepID uuid.UUID) (int64, error)
	// RevokeAllSessions signs out everything, including the caller.
	RevokeAllSessions(ctx context.Context, userID uuid.UUID) error
}

const sessionCols = `id, user_id, device_name, platform, app_version, created_at, last_seen_at`

func nilIfBlank(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *pgxStore) CreateSession(
	ctx context.Context, userID uuid.UUID, info DeviceInfo,
) (*Session, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, device_name, platform, app_version)
		VALUES ($1, $2, $3, $4)
		RETURNING `+sessionCols,
		userID, nilIfBlank(info.DeviceName), nilIfBlank(info.Platform),
		nilIfBlank(info.AppVersion),
	)
	var sess Session
	if err := row.Scan(
		&sess.ID, &sess.UserID, &sess.DeviceName, &sess.Platform,
		&sess.AppVersion, &sess.CreatedAt, &sess.LastSeenAt,
	); err != nil {
		return nil, err
	}
	return &sess, nil
}

// IsSessionLive runs on every authenticated request, so it is a primary-key
// lookup and nothing else. Without it, signing a device out would only stop it
// at the next token rotation — leaving whoever holds that device up to a full
// access-token lifetime (15 minutes) of unrestricted API access after you
// pressed the button that says it is signed out.
func (s *pgxStore) IsSessionLive(ctx context.Context, id uuid.UUID) (bool, error) {
	var live bool
	err := s.pool.QueryRow(ctx,
		`SELECT revoked_at IS NULL FROM sessions WHERE id = $1`, id,
	).Scan(&live)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return live, nil
}

func (s *pgxStore) ListSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+sessionCols+`
		  FROM sessions
		 WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY last_seen_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Empty slice, not nil, so the JSON is [] rather than null.
	sessions := []Session{}
	for rows.Next() {
		var sess Session
		if err := rows.Scan(
			&sess.ID, &sess.UserID, &sess.DeviceName, &sess.Platform,
			&sess.AppVersion, &sess.CreatedAt, &sess.LastSeenAt,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *pgxStore) TouchSession(ctx context.Context, id uuid.UUID, info DeviceInfo) (bool, error) {
	// COALESCE so a refresh that arrives without device headers keeps whatever
	// the sign-in recorded rather than blanking the row.
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions
		   SET last_seen_at = NOW(),
		       device_name  = COALESCE($2, device_name),
		       platform     = COALESCE($3, platform),
		       app_version  = COALESCE($4, app_version)
		 WHERE id = $1 AND revoked_at IS NULL`,
		id, nilIfBlank(info.DeviceName), nilIfBlank(info.Platform),
		nilIfBlank(info.AppVersion),
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *pgxStore) RevokeSession(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	// The refresh tokens go with it — marking the session revoked alone would
	// leave a live token able to mint access tokens for a signed-out device.
	// ON DELETE CASCADE handles the tokens once the session row is gone, but
	// the row is kept, so delete them explicitly.
	tag, err := s.pool.Exec(ctx, `
		WITH revoked AS (
			UPDATE sessions SET revoked_at = NOW()
			 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
			 RETURNING id
		)
		DELETE FROM refresh_tokens
		 WHERE session_id IN (SELECT id FROM revoked)`,
		id, userID,
	)
	if err != nil {
		return false, err
	}
	// RowsAffected counts deleted tokens, which is 0 for a session whose token
	// already expired. Re-read to find out whether the session itself matched.
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	var revoked bool
	if err := s.pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&revoked); err != nil {
		return false, nil // no such session for this user
	}
	return revoked, nil
}

func (s *pgxStore) RevokeOtherSessions(
	ctx context.Context, userID, keepID uuid.UUID,
) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		WITH revoked AS (
			UPDATE sessions SET revoked_at = NOW()
			 WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL
			 RETURNING id
		), dropped AS (
			DELETE FROM refresh_tokens
			 WHERE user_id = $1 AND session_id <> $2
		)
		SELECT count(*) FROM revoked`,
		userID, keepID,
	).Scan(&count)
	return count, err
}

func (s *pgxStore) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		WITH revoked AS (
			UPDATE sessions SET revoked_at = NOW()
			 WHERE user_id = $1 AND revoked_at IS NULL
		)
		DELETE FROM refresh_tokens WHERE user_id = $1`,
		userID,
	)
	return err
}

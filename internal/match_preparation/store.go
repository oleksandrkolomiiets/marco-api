package match_preparation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("preparation not found")

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

type CreateParams struct {
	UserID      uuid.UUID
	ScheduledAt time.Time
	Opponents   []string
	PartnerName *string
	Court       *string
	Note        *string
	Drills      []DrillInput
	// MessageID is the assistant chat message that spawned this prep, when the
	// create came from a [MATCH_PREP: ...] tag in chat. Nil for taps from the
	// Match Prep tab. Stored on match_preparation.message_id so the chat UI
	// can keep "Prep ready" across app restarts.
	MessageID *uuid.UUID
}

type DrillInput struct {
	Title           string
	DurationSeconds int
	// Completed lets the client carry the per-row done flag through a queue
	// replace. The handler always sets this for PUT /drills (the editor sheet
	// is authoritative on completion) and leaves it false for fresh creates.
	Completed bool
}

type UpdateParams struct {
	UserID      uuid.UUID
	ID          uuid.UUID
	ScheduledAt *time.Time
	Opponents   *[]string
	PartnerName *string
	Court       *string
	Note        *string
	MatchLogID  *uuid.UUID
	ClearMatch  bool // when true, sets match_log_id NULL regardless of MatchLogID
	PlanGrade   *string
	ClearGrade  bool
	PlayedAt    *time.Time
	ClearPlayed bool // when true, sets played_at NULL — the "unmark as played" path
}

// Create inserts a preparation row plus its drill queue in a single transaction.
// Drills are stored in the order given; Position is assigned 0..n-1.
func (s *Store) Create(ctx context.Context, p CreateParams) (Preparation, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preparation{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	opponents := p.Opponents
	if opponents == nil {
		opponents = []string{}
	}

	var r Preparation
	row := tx.QueryRow(ctx, `
		INSERT INTO match_preparation (user_id, scheduled_at, opponents, partner_name, court, note, message_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, match_log_id, message_id, scheduled_at, played_at, opponents, partner_name, court, note, plan_grade, created_at, updated_at
	`, p.UserID, p.ScheduledAt, opponents, p.PartnerName, p.Court, p.Note, p.MessageID)
	if err := row.Scan(
		&r.ID, &r.UserID, &r.MatchLogID, &r.MessageID, &r.ScheduledAt, &r.PlayedAt, &r.Opponents,
		&r.PartnerName, &r.Court, &r.Note, &r.PlanGrade, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return Preparation{}, fmt.Errorf("insert preparation: %w", err)
	}

	drills, err := insertDrills(ctx, tx, r.ID, p.Drills)
	if err != nil {
		return Preparation{}, err
	}
	r.Drills = drills
	r.PreparationPct = computePreparationPct(drills)

	if err := tx.Commit(ctx); err != nil {
		return Preparation{}, fmt.Errorf("commit tx: %w", err)
	}
	return r, nil
}

// insertDrills writes the queue rows for a preparation. Position is index-based so
// the front-end's drag-reorder can re-PUT the whole list without per-row diffs.
// Completed comes from the caller — fresh creates pass false, edits forward the
// authoritative state from the client.
func insertDrills(ctx context.Context, tx pgx.Tx, preparationID uuid.UUID, inputs []DrillInput) ([]Drill, error) {
	drills := make([]Drill, 0, len(inputs))
	for i, in := range inputs {
		var d Drill
		row := tx.QueryRow(ctx, `
			INSERT INTO match_preparation_drills (match_preparation_id, position, title, duration_seconds, completed)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, position, title, duration_seconds, completed, created_at
		`, preparationID, i, in.Title, in.DurationSeconds, in.Completed)
		if err := row.Scan(&d.ID, &d.Position, &d.Title, &d.DurationSeconds, &d.Completed, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("insert drill %d: %w", i, err)
		}
		drills = append(drills, d)
	}
	return drills, nil
}

// Get returns one preparation with its drills, scoped to the owning user.
func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Preparation, error) {
	var r Preparation
	row := s.db.QueryRow(ctx, `
		SELECT id, user_id, match_log_id, message_id, scheduled_at, played_at, opponents, partner_name, court, note, plan_grade, created_at, updated_at
		FROM match_preparation
		WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err := row.Scan(
		&r.ID, &r.UserID, &r.MatchLogID, &r.MessageID, &r.ScheduledAt, &r.PlayedAt, &r.Opponents,
		&r.PartnerName, &r.Court, &r.Note, &r.PlanGrade, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preparation{}, ErrNotFound
		}
		return Preparation{}, fmt.Errorf("get preparation: %w", err)
	}

	drills, err := s.listDrills(ctx, r.ID)
	if err != nil {
		return Preparation{}, err
	}
	r.Drills = drills
	r.PreparationPct = computePreparationPct(drills)
	return r, nil
}

// List returns all of a user's preparation rows with drills nested, ordered with
// upcoming matches first (soonest at top), then most recently-played matches.
// We sort by scheduled_at DESC so the freshest upcoming prep is always visible,
// and the client splits past vs future client-side.
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Preparation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, user_id, match_log_id, message_id, scheduled_at, played_at, opponents, partner_name, court, note, plan_grade, created_at, updated_at
		FROM match_preparation
		WHERE user_id = $1
		ORDER BY scheduled_at DESC, created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list preparation: %w", err)
	}
	defer rows.Close()

	out := []Preparation{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var r Preparation
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.MatchLogID, &r.MessageID, &r.ScheduledAt, &r.PlayedAt, &r.Opponents,
			&r.PartnerName, &r.Court, &r.Note, &r.PlanGrade, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan preparation: %w", err)
		}
		r.Drills = []Drill{}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate preparation: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}

	// One query to fetch every drill for the page — avoids the N+1 trap.
	drillRows, err := s.db.Query(ctx, `
		SELECT id, match_preparation_id, position, title, duration_seconds, completed, created_at
		FROM match_preparation_drills
		WHERE match_preparation_id = ANY($1)
		ORDER BY match_preparation_id, position
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("list drills: %w", err)
	}
	defer drillRows.Close()

	byPreparation := map[uuid.UUID][]Drill{}
	for drillRows.Next() {
		var rid uuid.UUID
		var d Drill
		if err := drillRows.Scan(&d.ID, &rid, &d.Position, &d.Title, &d.DurationSeconds, &d.Completed, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan drill: %w", err)
		}
		byPreparation[rid] = append(byPreparation[rid], d)
	}
	if err := drillRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drills: %w", err)
	}
	for i := range out {
		if ds, ok := byPreparation[out[i].ID]; ok {
			out[i].Drills = ds
		}
		out[i].PreparationPct = computePreparationPct(out[i].Drills)
	}
	return out, nil
}

func (s *Store) listDrills(ctx context.Context, preparationID uuid.UUID) ([]Drill, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, position, title, duration_seconds, completed, created_at
		FROM match_preparation_drills
		WHERE match_preparation_id = $1
		ORDER BY position
	`, preparationID)
	if err != nil {
		return nil, fmt.Errorf("list drills: %w", err)
	}
	defer rows.Close()

	out := []Drill{}
	for rows.Next() {
		var d Drill
		if err := rows.Scan(&d.ID, &d.Position, &d.Title, &d.DurationSeconds, &d.Completed, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan drill: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drills: %w", err)
	}
	return out, nil
}

// Update applies a partial patch to a preparation row. Pointer fields = "set if
// non-nil"; the ClearMatch/ClearGrade booleans cover the explicit-null case the
// caller may want when un-linking a match or rolling back a grade.
func (s *Store) Update(ctx context.Context, p UpdateParams) (Preparation, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preparation{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Use COALESCE-style updates so we touch only the fields the caller passed.
	const q = `
		UPDATE match_preparation SET
			scheduled_at = COALESCE($3, scheduled_at),
			opponents    = COALESCE($4, opponents),
			partner_name = CASE WHEN $5::bool THEN $6 ELSE partner_name END,
			court        = CASE WHEN $7::bool THEN $8 ELSE court END,
			note         = CASE WHEN $9::bool THEN $10 ELSE note END,
			match_log_id = CASE WHEN $11::bool THEN $12 ELSE match_log_id END,
			plan_grade   = CASE WHEN $13::bool THEN $14 ELSE plan_grade END,
			played_at    = CASE WHEN $15::bool THEN $16 ELSE played_at END
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, match_log_id, message_id, scheduled_at, played_at, opponents, partner_name, court, note, plan_grade, created_at, updated_at
	`

	setPartner := p.PartnerName != nil
	setCourt := p.Court != nil
	setNote := p.Note != nil
	setMatch := p.MatchLogID != nil || p.ClearMatch
	setGrade := p.PlanGrade != nil || p.ClearGrade
	setPlayed := p.PlayedAt != nil || p.ClearPlayed

	var opponentsArg interface{}
	if p.Opponents != nil {
		opponentsArg = *p.Opponents
	}

	var matchArg interface{}
	if p.MatchLogID != nil {
		matchArg = *p.MatchLogID
	}

	var playedArg interface{}
	if p.PlayedAt != nil {
		playedArg = *p.PlayedAt
	}

	var r Preparation
	row := tx.QueryRow(ctx, q,
		p.ID,
		p.UserID,
		p.ScheduledAt,
		opponentsArg,
		setPartner, p.PartnerName,
		setCourt, p.Court,
		setNote, p.Note,
		setMatch, matchArg,
		setGrade, p.PlanGrade,
		setPlayed, playedArg,
	)
	if err := row.Scan(
		&r.ID, &r.UserID, &r.MatchLogID, &r.MessageID, &r.ScheduledAt, &r.PlayedAt, &r.Opponents,
		&r.PartnerName, &r.Court, &r.Note, &r.PlanGrade, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preparation{}, ErrNotFound
		}
		return Preparation{}, fmt.Errorf("update preparation: %w", err)
	}

	drillRows, err := tx.Query(ctx, `
		SELECT id, position, title, duration_seconds, completed, created_at
		FROM match_preparation_drills
		WHERE match_preparation_id = $1
		ORDER BY position
	`, r.ID)
	if err != nil {
		return Preparation{}, fmt.Errorf("list drills: %w", err)
	}
	drills := []Drill{}
	for drillRows.Next() {
		var d Drill
		if err := drillRows.Scan(&d.ID, &d.Position, &d.Title, &d.DurationSeconds, &d.Completed, &d.CreatedAt); err != nil {
			drillRows.Close()
			return Preparation{}, fmt.Errorf("scan drill: %w", err)
		}
		drills = append(drills, d)
	}
	drillRows.Close()
	r.Drills = drills
	r.PreparationPct = computePreparationPct(drills)

	if err := tx.Commit(ctx); err != nil {
		return Preparation{}, fmt.Errorf("commit tx: %w", err)
	}
	return r, nil
}

// ReplaceDrills wipes a preparation's queue and re-inserts the given inputs in
// order. The client is the source of truth for each row's completed flag — it
// loads drills from the server, mutates locally as the user checks/unchecks,
// then PUTs the whole queue back. Replacing rather than diffing keeps the API
// stateless and avoids stale per-drill IDs surviving across reorders.
func (s *Store) ReplaceDrills(ctx context.Context, userID, preparationID uuid.UUID, inputs []DrillInput) (Preparation, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preparation{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Ownership check + lock the row.
	var ownerID uuid.UUID
	row := tx.QueryRow(ctx, `SELECT user_id FROM match_preparation WHERE id = $1 FOR UPDATE`, preparationID)
	if err := row.Scan(&ownerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preparation{}, ErrNotFound
		}
		return Preparation{}, fmt.Errorf("lock preparation: %w", err)
	}
	if ownerID != userID {
		return Preparation{}, ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM match_preparation_drills WHERE match_preparation_id = $1`, preparationID); err != nil {
		return Preparation{}, fmt.Errorf("delete drills: %w", err)
	}

	if _, err := insertDrills(ctx, tx, preparationID, inputs); err != nil {
		return Preparation{}, err
	}

	// Bump updated_at on the parent so the client's list refreshes.
	if _, err := tx.Exec(ctx, `UPDATE match_preparation SET updated_at = NOW() WHERE id = $1`, preparationID); err != nil {
		return Preparation{}, fmt.Errorf("touch preparation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Preparation{}, fmt.Errorf("commit tx: %w", err)
	}

	// Re-read the row so the caller gets fresh updated_at.
	return s.Get(ctx, userID, preparationID)
}

// SetDrillCompleted flips the done flag on one drill, scoped by user via the
// parent preparation — so a poisoned drill ID can't toggle someone else's queue.
func (s *Store) SetDrillCompleted(ctx context.Context, userID, drillID uuid.UUID, completed bool) (Drill, error) {
	const q = `
		UPDATE match_preparation_drills d
		SET completed = $3
		FROM match_preparation r
		WHERE d.id = $1 AND d.match_preparation_id = r.id AND r.user_id = $2
		RETURNING d.id, d.position, d.title, d.duration_seconds, d.completed, d.created_at
	`
	var d Drill
	row := s.db.QueryRow(ctx, q, drillID, userID, completed)
	if err := row.Scan(&d.ID, &d.Position, &d.Title, &d.DurationSeconds, &d.Completed, &d.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Drill{}, ErrNotFound
		}
		return Drill{}, fmt.Errorf("toggle drill: %w", err)
	}
	return d, nil
}

// Delete removes a preparation (and its drills via ON DELETE CASCADE).
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error {
	cmd, err := s.db.Exec(ctx, `DELETE FROM match_preparation WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete preparation: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
)

// Profile is one user's scoring profile and filter preferences.
type Profile struct {
	UserID      int64              `json:"-"`
	Markdown    string             `json:"markdown"`
	ProfileHash string             `json:"-"`
	Preferences config.Preferences `json:"preferences"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// Ready reports whether the pipeline can match jobs for this profile:
// it needs profile text and at least one role family and level.
func (p *Profile) Ready() bool {
	return p.Markdown != "" && len(p.Preferences.RoleFamilies) > 0 && len(p.Preferences.Levels) > 0
}

const profileColumns = `user_id, markdown, profile_hash, preferences, updated_at`

func scanProfile(row pgx.Row) (*Profile, error) {
	var p Profile
	var prefs []byte
	if err := row.Scan(&p.UserID, &p.Markdown, &p.ProfileHash, &prefs, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(prefs, &p.Preferences); err != nil {
		return nil, fmt.Errorf("profile %d preferences: %w", p.UserID, err)
	}
	return &p, nil
}

// GetProfile returns a user's profile, or nil when they have none yet.
func GetProfile(ctx context.Context, pool *pgxpool.Pool, userID int64) (*Profile, error) {
	p, err := scanProfile(pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM profiles WHERE user_id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ReadyProfiles returns every profile the pipeline can match jobs for.
func ReadyProfiles(ctx context.Context, pool *pgxpool.Pool) ([]*Profile, error) {
	rows, err := pool.Query(ctx, `SELECT `+profileColumns+` FROM profiles WHERE markdown <> '' ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		if p.Ready() {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// SaveProfile upserts a user's profile. When the preferences changed, every
// rule outcome for the user is invalidated so the next filter pass
// re-applies the rules (cheap; scores for an unchanged profile text are
// reused). When only the text changed, scored jobs are requeued so they
// are scored against the new text.
func SaveProfile(ctx context.Context, pool *pgxpool.Pool, userID int64, markdown, profileHash string, prefs config.Preferences) error {
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var oldHash string
	var oldPrefs []byte
	err = tx.QueryRow(ctx, `SELECT profile_hash, preferences FROM profiles WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&oldHash, &oldPrefs)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profiles (user_id, markdown, profile_hash, preferences, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id) DO UPDATE
		SET markdown = EXCLUDED.markdown, profile_hash = EXCLUDED.profile_hash,
		    preferences = EXCLUDED.preferences, updated_at = now()`,
		userID, markdown, profileHash, prefsJSON); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}

	var old config.Preferences
	_ = json.Unmarshal(oldPrefs, &old)
	switch {
	case !samePreferences(old, prefs):
		if _, err := tx.Exec(ctx, `UPDATE user_jobs SET content_hash = '' WHERE user_id = $1`, userID); err != nil {
			return err
		}
	case oldHash != profileHash:
		if _, err := tx.Exec(ctx, `
			UPDATE user_jobs SET stage = 'pending_score', updated_at = now()
			WHERE user_id = $1 AND stage IN ('scored','score_failed')`, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func samePreferences(a, b config.Preferences) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

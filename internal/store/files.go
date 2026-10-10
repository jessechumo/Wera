package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SaveAvatar stores a user's (already normalized) profile picture.
func SaveAvatar(ctx context.Context, pool *pgxpool.Pool, userID int64, contentType string, data []byte) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO avatars (user_id, content_type, data) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET content_type = EXCLUDED.content_type, data = EXCLUDED.data, updated_at = now()`,
		userID, contentType, data)
	return err
}

// Avatar returns a user's profile picture; ok is false when there is none.
func Avatar(ctx context.Context, pool *pgxpool.Pool, userID int64) (contentType string, data []byte, updated time.Time, ok bool, err error) {
	err = pool.QueryRow(ctx, `SELECT content_type, data, updated_at FROM avatars WHERE user_id = $1`, userID).
		Scan(&contentType, &data, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, time.Time{}, false, nil
	}
	return contentType, data, updated, err == nil, err
}

// DeleteAvatar removes a user's profile picture.
func DeleteAvatar(ctx context.Context, pool *pgxpool.Pool, userID int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM avatars WHERE user_id = $1`, userID)
	return err
}

// AvatarVersion returns the unix time of a user's picture (nil if none),
// used to bust caches when it changes.
func AvatarVersion(ctx context.Context, pool *pgxpool.Pool, userID int64) (*int64, error) {
	var t time.Time
	err := pool.QueryRow(ctx, `SELECT updated_at FROM avatars WHERE user_id = $1`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := t.Unix()
	return &v, nil
}

// SaveResumeFile stores the uploaded resume PDF.
func SaveResumeFile(ctx context.Context, pool *pgxpool.Pool, userID int64, filename string, data []byte) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO resume_files (user_id, filename, data) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET filename = EXCLUDED.filename, data = EXCLUDED.data, uploaded_at = now()`,
		userID, filename, data)
	return err
}

// ResumeFile returns the stored resume PDF; ok is false when there is none.
func ResumeFile(ctx context.Context, pool *pgxpool.Pool, userID int64) (filename string, data []byte, uploaded time.Time, ok bool, err error) {
	err = pool.QueryRow(ctx, `SELECT filename, data, uploaded_at FROM resume_files WHERE user_id = $1`, userID).
		Scan(&filename, &data, &uploaded)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, time.Time{}, false, nil
	}
	return filename, data, uploaded, err == nil, err
}

// ResumeFileInfo returns the stored resume's name and upload time (ok
// false when there is none) without loading the file.
func ResumeFileInfo(ctx context.Context, pool *pgxpool.Pool, userID int64) (filename string, uploaded time.Time, ok bool, err error) {
	err = pool.QueryRow(ctx, `SELECT filename, uploaded_at FROM resume_files WHERE user_id = $1`, userID).
		Scan(&filename, &uploaded)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	return filename, uploaded, err == nil, err
}

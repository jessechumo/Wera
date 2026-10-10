package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Author is the public face of a user on the blog.
type Author struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	AvatarVersion *int64 `json:"avatar_version"`
}

// PostSummary is a post in the feed.
type PostSummary struct {
	ID          int64            `json:"id"`
	Title       string           `json:"title"`
	Excerpt     string           `json:"excerpt"`
	Tags        []string         `json:"tags"`
	Author      Author           `json:"author"`
	CreatedAt   time.Time        `json:"created_at"`
	ReadMinutes int              `json:"read_minutes"`
	Reactions   map[string]int64 `json:"reactions"`
	Mine        []string         `json:"my_reactions"`
	Comments    int64            `json:"comment_count"`
	IsMine      bool             `json:"is_mine"`
}

// Comment is one published comment.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	Author    Author    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	IsMine    bool      `json:"is_mine"`
}

// PostView is one post with its body and comments.
type PostView struct {
	PostSummary
	Body         string    `json:"body"`
	CommentsList []Comment `json:"comments"`
}

const authorCols = `u.id, CASE WHEN u.name <> '' THEN u.name ELSE split_part(u.email, '@', 1) END,
	(SELECT extract(epoch FROM a.updated_at)::bigint FROM avatars a WHERE a.user_id = u.id)`

const postSummarySelect = `
	SELECT p.id, p.title, left(p.body, 280), p.tags, ` + authorCols + `, p.created_at,
	       greatest(1, round(length(p.body) / 1100.0))::int,
	       (SELECT coalesce(jsonb_object_agg(kind, n), '{}') FROM
	          (SELECT kind, count(*) n FROM post_reactions r WHERE r.post_id = p.id GROUP BY kind) k),
	       ARRAY(SELECT kind FROM post_reactions r WHERE r.post_id = p.id AND r.user_id = $1),
	       (SELECT count(*) FROM post_comments c WHERE c.post_id = p.id AND c.status = 'published'),
	       p.user_id = $1
	FROM posts p JOIN users u ON u.id = p.user_id
	WHERE p.status = 'published'`

func scanSummary(row pgx.Row) (*PostSummary, error) {
	var s PostSummary
	var reactions []byte
	if err := row.Scan(&s.ID, &s.Title, &s.Excerpt, &s.Tags, &s.Author.ID, &s.Author.Name, &s.Author.AvatarVersion,
		&s.CreatedAt, &s.ReadMinutes, &reactions, &s.Mine, &s.Comments, &s.IsMine); err != nil {
		return nil, err
	}
	s.Reactions = map[string]int64{}
	_ = json.Unmarshal(reactions, &s.Reactions)
	return &s, nil
}

// CreatePost stores a moderated post and returns its id.
func CreatePost(ctx context.Context, pool *pgxpool.Pool, userID int64, title, body string, tags []string, published bool, verdict any) (int64, error) {
	status := "rejected"
	if published {
		status = "published"
	}
	raw, _ := json.Marshal(verdict)
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO posts (user_id, title, body, tags, status, moderation) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, userID, title, body, tags, status, raw).Scan(&id)
	return id, err
}

// ListPosts returns published posts, newest first, optionally by tag.
func ListPosts(ctx context.Context, pool *pgxpool.Pool, viewerID int64, tag string, limit, offset int) ([]PostSummary, error) {
	q := postSummarySelect
	args := []any{viewerID}
	if tag != "" {
		args = append(args, tag)
		q += fmt.Sprintf(" AND $%d = ANY(p.tags)", len(args))
	}
	args = append(args, normalizeLimit(limit, 20, 50), max(offset, 0))
	q += fmt.Sprintf(" ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PostSummary{}
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetPost returns one published post with its comments (pgx.ErrNoRows
// when missing or not published).
func GetPost(ctx context.Context, pool *pgxpool.Pool, viewerID, id int64) (*PostView, error) {
	s, err := scanSummary(pool.QueryRow(ctx, postSummarySelect+" AND p.id = $2", viewerID, id))
	if err != nil {
		return nil, err
	}
	v := &PostView{PostSummary: *s, CommentsList: []Comment{}}
	if err := pool.QueryRow(ctx, `SELECT body FROM posts WHERE id = $1`, id).Scan(&v.Body); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.body, `+authorCols+`, c.created_at, c.user_id = $2
		FROM post_comments c JOIN users u ON u.id = c.user_id
		WHERE c.post_id = $1 AND c.status = 'published' ORDER BY c.created_at`, id, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.Body, &c.Author.ID, &c.Author.Name, &c.Author.AvatarVersion, &c.CreatedAt, &c.IsMine); err != nil {
			return nil, err
		}
		v.CommentsList = append(v.CommentsList, c)
	}
	return v, rows.Err()
}

// ErrNotAllowed is returned when a user may not change someone's content.
var ErrNotAllowed = errors.New("not allowed")

// DeletePost removes a post (its author or an admin).
func DeletePost(ctx context.Context, pool *pgxpool.Pool, userID int64, isAdmin bool, id int64) error {
	tag, err := pool.Exec(ctx, `DELETE FROM posts WHERE id = $1 AND ($2 OR user_id = $3)`, id, isAdmin, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotAllowed
	}
	return nil
}

// CreateComment stores a moderated comment on a published post.
func CreateComment(ctx context.Context, pool *pgxpool.Pool, userID, postID int64, body string, published bool, verdict any) (int64, error) {
	status := "rejected"
	if published {
		status = "published"
	}
	raw, _ := json.Marshal(verdict)
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO post_comments (post_id, user_id, body, status, moderation)
		SELECT id, $2, $3, $4, $5 FROM posts WHERE id = $1 AND status = 'published'
		RETURNING id`, postID, userID, body, status, raw).Scan(&id)
	return id, err
}

// DeleteComment removes a comment (its author or an admin).
func DeleteComment(ctx context.Context, pool *pgxpool.Pool, userID int64, isAdmin bool, id int64) error {
	tag, err := pool.Exec(ctx, `DELETE FROM post_comments WHERE id = $1 AND ($2 OR user_id = $3)`, id, isAdmin, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotAllowed
	}
	return nil
}

// ValidReactions are the reaction kinds.
var ValidReactions = map[string]bool{"like": true, "insightful": true, "celebrate": true}

// SetReaction adds or removes one of the user's reactions on a published post.
func SetReaction(ctx context.Context, pool *pgxpool.Pool, userID, postID int64, kind string, on bool) error {
	var err error
	if on {
		_, err = pool.Exec(ctx, `
			INSERT INTO post_reactions (post_id, user_id, kind)
			SELECT id, $2, $3 FROM posts WHERE id = $1 AND status = 'published'
			ON CONFLICT DO NOTHING`, postID, userID, kind)
	} else {
		_, err = pool.Exec(ctx, `DELETE FROM post_reactions WHERE post_id = $1 AND user_id = $2 AND kind = $3`, postID, userID, kind)
	}
	return err
}

// PostTags lists tags in use with their post counts, most used first.
func PostTags(ctx context.Context, pool *pgxpool.Pool) ([]map[string]any, error) {
	rows, err := pool.Query(ctx, `
		SELECT tag, count(*) FROM posts, unnest(tags) tag
		WHERE status = 'published' GROUP BY tag ORDER BY count(*) DESC, tag LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var t string
		var n int64
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"tag": t, "count": n})
	}
	return out, rows.Err()
}

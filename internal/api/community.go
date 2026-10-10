package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"wera/internal/moderation"
	"wera/internal/pipeline"
	"wera/internal/store"
)

// moderate runs the rule checks, then the AI review (or s.Moderator in
// tests). It fails closed: when no verdict can be had, nothing is posted.
func (s *Server) moderate(ctx context.Context, userID int64, kind moderation.Kind, title, body string) (moderation.Verdict, error) {
	if v, ok := moderation.CheckRules(kind, title, body); !ok {
		return v, nil
	}
	if s.Moderator != nil {
		return s.Moderator(ctx, kind, title, body)
	}
	if s.Env == nil || s.Env.CoralAPIKey == "" {
		return moderation.Verdict{}, errors.New("moderation is not configured")
	}
	if allowance, _, err := pipeline.Allowance(ctx, s.Pool, s.Env, userID); err != nil || allowance <= 0 {
		return moderation.Verdict{}, errors.New("posting is unavailable right now")
	}
	reply, err := s.chatForUser(ctx, userID, "moderation", 300, moderation.Messages(kind, title, body))
	if err != nil {
		return moderation.Verdict{}, err
	}
	return moderation.ParseVerdict(reply)
}

var tagRE = regexp.MustCompile(`[^a-z0-9-]+`)

// cleanTags lowercases, slugs and dedupes up to 5 tags.
func cleanTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.Trim(tagRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(t)), "-"), "-")
		if len(t) > 24 {
			t = t[:24]
		}
		if t != "" && !seen[t] && len(out) < 5 {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) listPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	posts, err := store.ListPosts(r.Context(), s.Pool, currentUser(r).ID, q.Get("tag"), limit, offset)
	if err != nil {
		s.Log.Error("list posts failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	tags, err := store.PostTags(r.Context(), s.Pool)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "tags": tags})
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad post id")
		return
	}
	p, err := store.GetPost(r.Context(), s.Pool, currentUser(r).ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

// createPost is POST /api/posts: moderated, then published or refused
// with the reason (422).
func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !s.postLimit.Allow(strconv.FormatInt(user.ID, 10)) {
		s.writeError(w, http.StatusTooManyRequests, "you are posting a lot; try again in an hour")
		return
	}
	var in struct {
		Title string   `json:"title"`
		Body  string   `json:"body"`
		Tags  []string `json:"tags"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	in.Title, in.Body = strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	v, err := s.moderate(r.Context(), user.ID, moderation.Post, in.Title, in.Body)
	if err != nil {
		s.Log.Warn("moderation failed", "user_id", user.ID, "err", err)
		s.writeError(w, http.StatusServiceUnavailable, "we could not review your post right now; try again shortly")
		return
	}
	if v.Source == "rules" && !v.Allowed {
		s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": v.Reason, "categories": v.Categories})
		return
	}
	id, err := store.CreatePost(r.Context(), s.Pool, user.ID, in.Title, in.Body, cleanTags(in.Tags), v.Allowed, v)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not save the post")
		return
	}
	if !v.Allowed {
		s.Log.Info("post rejected by moderation", "user_id", user.ID, "post_id", id, "categories", v.Categories)
		s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": v.Reason, "categories": v.Categories})
		return
	}
	p, err := store.GetPost(r.Context(), s.Pool, user.ID, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusCreated, p)
}

func (s *Server) deletePost(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad post id")
		return
	}
	u := currentUser(r)
	if err := store.DeletePost(r.Context(), s.Pool, u.ID, u.IsAdmin, id); errors.Is(err, store.ErrNotAllowed) {
		s.writeError(w, http.StatusForbidden, "you can only delete your own posts")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad post id")
		return
	}
	user := currentUser(r)
	if !s.commentLimit.Allow(strconv.FormatInt(user.ID, 10)) {
		s.writeError(w, http.StatusTooManyRequests, "slow down a little; try again soon")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	v, err := s.moderate(r.Context(), user.ID, moderation.Comment, "", in.Body)
	if err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "we could not review your comment right now; try again shortly")
		return
	}
	if v.Source == "rules" && !v.Allowed {
		s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": v.Reason, "categories": v.Categories})
		return
	}
	if _, err := store.CreateComment(r.Context(), s.Pool, user.ID, id, in.Body, v.Allowed, v); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "post not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not save the comment")
		return
	}
	if !v.Allowed {
		s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": v.Reason, "categories": v.Categories})
		return
	}
	s.getPost(w, r)
}

func (s *Server) deleteComment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "commentID"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad comment id")
		return
	}
	u := currentUser(r)
	if err := store.DeleteComment(r.Context(), s.Pool, u.ID, u.IsAdmin, id); errors.Is(err, store.ErrNotAllowed) {
		s.writeError(w, http.StatusForbidden, "you can only delete your own comments")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// react is PUT (add) / DELETE (remove) /api/posts/{id}/reactions/{kind}.
func (s *Server) react(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	kind := chi.URLParam(r, "kind")
	if !ok || !store.ValidReactions[kind] {
		s.writeError(w, http.StatusBadRequest, "bad reaction")
		return
	}
	if err := store.SetReaction(r.Context(), s.Pool, currentUser(r).ID, id, kind, r.Method == http.MethodPut); err != nil {
		s.writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

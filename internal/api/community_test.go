package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"wera/internal/moderation"
)

// fakeModerator rejects anything mentioning "punch" and allows the rest.
func fakeModerator(_ context.Context, _ moderation.Kind, title, body string) (moderation.Verdict, error) {
	if strings.Contains(strings.ToLower(title+body), "punch") {
		return moderation.Verdict{Categories: []string{"violence"}, Reason: "Threatens violence.", Source: "ai"}, nil
	}
	return moderation.Verdict{Allowed: true, Source: "ai"}, nil
}

func TestBlogFlow(t *testing.T) {
	ts, pool := testServer(t, nil)
	body := strings.Repeat("Practice the STAR format out loud before every interview. ", 6)
	post := func(title, text string) (int, string) {
		b, _ := json.Marshal(map[string]any{"title": title, "body": text, "tags": []string{"Interviews", "interviews", "Big Tech!"}})
		return do(t, client, http.MethodPost, ts.URL+"/api/posts", string(b))
	}

	if code, out := post("Hi", body); code != 422 || !strings.Contains(out, "Titles") {
		t.Errorf("rule rejection: %d %s", code, out)
	}
	if code, out := post("How to handle a rude interviewer", body+" I wanted to punch him."); code != 422 || !strings.Contains(out, "violence") {
		t.Errorf("moderation rejection: %d %s", code, out)
	}
	var rejected int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM posts WHERE user_id = $1 AND status = 'rejected'`, testUserID).Scan(&rejected)
	if rejected != 1 {
		t.Errorf("rejected post not kept for audit: %d", rejected)
	}

	code, out := post("How I prepared for behavioral rounds", body)
	if code != 201 {
		t.Fatalf("publish: %d %s", code, out)
	}
	var p struct {
		ID   int64    `json:"id"`
		Tags []string `json:"tags"`
	}
	json.Unmarshal([]byte(out), &p)
	if strings.Join(p.Tags, ",") != "interviews,big-tech" {
		t.Errorf("tags not cleaned: %v", p.Tags)
	}
	url := fmt.Sprintf("%s/api/posts/%d", ts.URL, p.ID)

	if _, out := do(t, client, http.MethodGet, ts.URL+"/api/posts", ""); !strings.Contains(out, "How I prepared") || strings.Contains(out, "rude interviewer") {
		t.Errorf("feed shows the wrong posts: %s", out)
	}
	if code, _ := do(t, client, http.MethodPut, url+"/reactions/insightful", ""); code != 204 {
		t.Errorf("react: %d", code)
	}
	if code, _ := do(t, client, http.MethodPut, url+"/reactions/angry", ""); code != 400 {
		t.Errorf("unknown reaction: want 400, got %d", code)
	}
	if code, out := do(t, client, http.MethodPost, url+"/comments", `{"body":"Great tips, thanks!"}`); code != 200 || !strings.Contains(out, "Great tips") {
		t.Errorf("comment: %d %s", code, out)
	}
	if code, _ := do(t, client, http.MethodPost, url+"/comments", `{"body":"I will punch you"}`); code != 422 {
		t.Errorf("abusive comment: want 422, got %d", code)
	}
	_, out = do(t, client, http.MethodGet, url, "")
	if !strings.Contains(out, `"insightful":1`) || !strings.Contains(out, `"comment_count":1`) || strings.Contains(out, "punch you") {
		t.Errorf("post view: %s", out)
	}
	if code, _ := do(t, client, http.MethodDelete, url, ""); code != 204 {
		t.Errorf("delete own post: %d", code)
	}
	if code, _ := do(t, client, http.MethodGet, url, ""); code != 404 {
		t.Errorf("deleted post: want 404, got %d", code)
	}
}

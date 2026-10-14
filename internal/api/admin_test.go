package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"wera/internal/store"
	"wera/internal/testutil"
)

func TestAdminUsersAndAudit(t *testing.T) {
	ts, pool := testServer(t, nil)
	ctx := context.Background()
	other := testutil.User(t, pool, false)
	userURL := fmt.Sprintf("%s/api/admin/users/%d", ts.URL, other.ID)

	if code, body := do(t, client, http.MethodGet, ts.URL+"/api/admin/overview", ""); code != 200 || !strings.Contains(body, `"users":`) {
		t.Fatalf("overview: %d %s", code, body)
	}
	if code, body := do(t, client, http.MethodGet, ts.URL+"/api/admin/users", ""); code != 200 || !strings.Contains(body, other.Email) {
		t.Fatalf("users: %d %.200s", code, body)
	}

	// Budget and role changes, with their limits.
	if code, body := do(t, client, http.MethodPatch, userURL, `{"monthly_budget_usd":25,"is_admin":true}`); code != 200 ||
		!strings.Contains(body, `"monthly_budget_usd":25`) || !strings.Contains(body, `"is_admin":true`) {
		t.Errorf("update: %d %s", code, body)
	}
	if code, _ := do(t, client, http.MethodPatch, userURL, `{"monthly_budget_usd":-1}`); code != 400 {
		t.Errorf("negative budget: want 400, got %d", code)
	}
	self := fmt.Sprintf("%s/api/admin/users/%d", ts.URL, testUserID)
	if code, _ := do(t, client, http.MethodPatch, self, `{"is_admin":false}`); code != 409 {
		t.Errorf("removing your own admin role: want 409, got %d", code)
	}
	if code, _ := do(t, client, http.MethodPatch, ts.URL+"/api/admin/users/999999999", `{"name":"x"}`); code != 404 {
		t.Errorf("unknown user: want 404, got %d", code)
	}

	// Sign out everywhere, then delete with the email as confirmation.
	store.CreateSession(ctx, pool, other.ID, "admin-test-session-"+testutil.Unique("s"), 3600e9)
	if code, body := do(t, client, http.MethodPost, userURL+"/sign-out", ""); code != 200 || !strings.Contains(body, `"ended":1`) {
		t.Errorf("sign out: %d %s", code, body)
	}
	if code, _ := do(t, client, http.MethodDelete, userURL, `{"confirm_email":"wrong@example.com"}`); code != 400 {
		t.Errorf("delete without the right email: want 400, got %d", code)
	}
	if code, _ := do(t, client, http.MethodDelete, self, `{"confirm_email":"x"}`); code != 409 {
		t.Errorf("deleting yourself: want 409, got %d", code)
	}
	if code, _ := do(t, client, http.MethodDelete, userURL, `{"confirm_email":"`+strings.ToUpper(other.Email)+`"}`); code != 204 {
		t.Errorf("delete: want 204, got %d", code)
	}

	_, body := do(t, client, http.MethodGet, ts.URL+"/api/admin/audit", "")
	for _, action := range []string{"user.update", "user.sign_out", "user.delete"} {
		if !strings.Contains(body, action) {
			t.Errorf("audit log misses %s: %.300s", action, body)
		}
	}
}

func TestAdminContentModeration(t *testing.T) {
	ts, pool := testServer(t, nil)
	ctx := context.Background()
	author := testutil.User(t, pool, false)
	postID, err := store.CreatePost(ctx, pool, author.ID, "Held post", "Body held by moderation.", []string{}, false, map[string]any{"reason": "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, body := do(t, client, http.MethodGet, ts.URL+"/api/admin/content?kind=post&status=rejected", "")
	if !strings.Contains(body, "Held post") || !strings.Contains(body, `"reason":"test"`) {
		t.Fatalf("held content: %.300s", body)
	}
	url := fmt.Sprintf("%s/api/admin/content/post/%d", ts.URL, postID)
	if code, _ := do(t, client, http.MethodPut, url+"/status", `{"status":"published"}`); code != 204 {
		t.Errorf("publish: want 204, got %d", code)
	}
	if code, _ := do(t, client, http.MethodPut, url+"/status", `{"status":"maybe"}`); code != 400 {
		t.Errorf("bad status: want 400, got %d", code)
	}
	if code, _ := do(t, client, http.MethodGet, ts.URL+"/api/admin/content?kind=page", ""); code != 400 {
		t.Errorf("bad kind: want 400, got %d", code)
	}
	if code, _ := do(t, client, http.MethodDelete, url, ""); code != 204 {
		t.Errorf("delete: want 204, got %d", code)
	}
	if code, _ := do(t, client, http.MethodDelete, url, ""); code != 404 {
		t.Errorf("delete again: want 404, got %d", code)
	}
}

func TestAdminRoutesNeedAnAdminSession(t *testing.T) {
	ts, pool := testServer(t, nil)
	// An extension token, even an admin's, never reaches admin routes.
	ext, email := connectExtension(t, ts.URL)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })
	pool.Exec(context.Background(), `UPDATE users SET is_admin = true WHERE email = $1`, email)
	if code, _ := do(t, ext, http.MethodGet, ts.URL+"/api/admin/users", ""); code != 403 {
		t.Errorf("extension token on an admin route: want 403, got %d", code)
	}
	anon := &http.Client{}
	if code, _ := do(t, anon, http.MethodGet, ts.URL+"/api/admin/overview", ""); code != 401 {
		t.Errorf("signed out: want 401, got %d", code)
	}
}

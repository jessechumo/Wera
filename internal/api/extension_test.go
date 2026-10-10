package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"wera/internal/auth"
	"wera/internal/config"
	"wera/internal/store"
	"wera/internal/testutil"
)

// extClient is how the extension calls the API: a bearer token and the
// chrome-extension:// origin (which the cookie origin check would refuse).
type extTransport struct{ token string }

func (t extTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// connectExtension signs a fresh user up and connects an extension.
func connectExtension(t *testing.T, base string) (*http.Client, string) {
	t.Helper()
	email := testutil.Unique("ext") + "@example.com"
	if code, body := do(t, &http.Client{}, http.MethodPost, base+"/api/auth/signup", `{"email":"`+email+`","password":"extension pass 1","name":"Ext User"}`); code != 201 {
		t.Fatalf("signup: %d %s", code, body)
	}
	anonExt := &http.Client{Transport: extTransport{}}
	if code, _ := do(t, anonExt, http.MethodPost, base+"/api/ext/tokens", `{"email":"`+email+`","password":"wrong password"}`); code != 401 {
		t.Errorf("wrong password: want 401, got %d", code)
	}
	code, body := do(t, anonExt, http.MethodPost, base+"/api/ext/tokens", `{"email":"`+email+`","password":"extension pass 1","name":"Chrome on laptop"}`)
	if code != 201 {
		t.Fatalf("connect: %d %s", code, body)
	}
	var out struct{ Token string }
	json.Unmarshal([]byte(body), &out)
	if !strings.HasPrefix(out.Token, "wera_ext_") {
		t.Fatalf("token: %s", body)
	}
	return &http.Client{Transport: extTransport{out.Token}}, email
}

func TestExtensionTokensAndAddJob(t *testing.T) {
	ts, pool := testServer(t, nil)
	ext, email := connectExtension(t, ts.URL)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })

	if code, body := do(t, ext, http.MethodGet, ts.URL+"/api/auth/me", ""); code != 200 || !strings.Contains(body, email) {
		t.Fatalf("me with token: %d %s", code, body)
	}

	add := `{"url":"https://jobs.example.com/acme/123?utm_source=linkedin","company":"Acme Robotics E2E","title":"Site Reliability Engineer",
	        "location":"Austin, TX","work_mode":"onsite","description":"Keep robots online.","industry":"robotics"}`
	code, body := do(t, ext, http.MethodPost, ts.URL+"/api/ext/jobs", add)
	if code != 201 || !strings.Contains(body, `"source":"manual"`) || !strings.Contains(body, `"application_status":"saved"`) {
		t.Fatalf("add job: %d %s", code, body)
	}
	var job struct{ ID, CompanyID int64 }
	json.Unmarshal([]byte(body), &job)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM companies WHERE id = $1 AND ats = 'manual'`, job.CompanyID)
	})
	if code, again := do(t, ext, http.MethodPost, ts.URL+"/api/ext/jobs", add); code != 200 || !strings.Contains(again, fmt.Sprintf(`"id":%d`, job.ID)) {
		t.Errorf("same URL again should return the same job: %d", code)
	}
	if code, _ := do(t, ext, http.MethodPost, ts.URL+"/api/ext/jobs", `{"url":"javascript:alert(1)","company":"A","title":"B"}`); code != 400 {
		t.Errorf("non-http URL: want 400, got %d", code)
	}
	_, body = do(t, ext, http.MethodGet, ts.URL+"/api/ext/jobs/lookup?url="+
		"https%3A%2F%2Fjobs.example.com%2Facme%2F123%2F%23apply", "")
	if !strings.Contains(body, fmt.Sprintf(`"id":%d`, job.ID)) {
		t.Errorf("lookup by URL failed: %s", body)
	}
	_, body = do(t, ext, http.MethodGet, ts.URL+"/api/ext/jobs/lookup?url="+
		"https%3A%2F%2Fjobs.example.com%2Facme%2F123%2Fapply", "")
	if !strings.Contains(body, fmt.Sprintf(`"id":%d`, job.ID)) {
		t.Errorf("lookup from the /apply page failed: %s", body)
	}
	if _, body = do(t, ext, http.MethodGet, ts.URL+"/api/ext/jobs/lookup?url=https%3A%2F%2Fnowhere.example%2Fx", ""); body != "{\"job\":null}\n" {
		t.Errorf("unknown URL: %s", body)
	}

	// Only its owner sees the job; the shared lists never do.
	if code, _ := do(t, client, http.MethodGet, fmt.Sprintf("%s/api/jobs/%d", ts.URL, job.ID), ""); code != 404 {
		t.Errorf("another user can see an added job: %d", code)
	}
	if _, body := do(t, client, http.MethodGet, ts.URL+"/api/companies", ""); strings.Contains(body, "Acme Robotics E2E") {
		t.Error("a manual company leaked into the shared company list")
	}

	// The dashboard lists connections; the extension can sign itself out.
	_, body = do(t, ext, http.MethodGet, ts.URL+"/api/ext/tokens", "")
	if !strings.Contains(body, "Chrome on laptop") {
		t.Errorf("connections: %s", body)
	}
	if code, _ := do(t, ext, http.MethodDelete, ts.URL+"/api/ext/tokens/current", ""); code != 204 {
		t.Errorf("sign out: %d", code)
	}
	if code, _ := do(t, ext, http.MethodGet, ts.URL+"/api/auth/me", ""); code != 401 {
		t.Errorf("revoked token still works: %d", code)
	}
}

func TestBearerTokensAreNotCookies(t *testing.T) {
	ts, _ := testServer(t, nil)
	bogus := &http.Client{Transport: extTransport{"wera_ext_not-a-real-token"}}
	if code, _ := do(t, bogus, http.MethodGet, ts.URL+"/api/auth/me", ""); code != 401 {
		t.Errorf("unknown token: want 401, got %d", code)
	}
	// A cookie session cannot be used from the extension origin without a token.
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/settings", strings.NewReader(`{}`))
	req.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("cookie write from another origin: want 403, got %d", resp.StatusCode)
	}
}

func TestApplicantDetails(t *testing.T) {
	ts, _ := testServer(t, nil)
	code, body := do(t, client, http.MethodGet, ts.URL+"/api/applicant", "")
	if code != 200 || !strings.Contains(body, `"saved":false`) || !strings.Contains(body, `"gender":"decline"`) || !strings.Contains(body, `"first_name":"API"`) {
		t.Fatalf("defaults: %d %s", code, body)
	}
	if code, _ := do(t, client, http.MethodPut, ts.URL+"/api/applicant", `{"needs_sponsorship":"maybe"}`); code != 400 {
		t.Errorf("bad answer: want 400, got %d", code)
	}
	code, body = do(t, client, http.MethodPut, ts.URL+"/api/applicant", `{"first_name":"Ada","phone":"512 555 0142","needs_sponsorship":"yes"}`)
	if code != 200 || !strings.Contains(body, `"saved":true`) || !strings.Contains(body, `"phone":"512 555 0142"`) {
		t.Errorf("save: %d %s", code, body)
	}
}

func TestExtensionAIFeatures(t *testing.T) {
	ts, _ := llmServer(t)
	pool := testPool(t)
	ctx := context.Background()
	testutil.Profile(t, pool, testUserID, "# Candidate Profile\n## Target roles\nSRE.", config.Preferences{RoleFamilies: []string{"sre"}, Levels: []string{"entry"}})
	pool.Exec(ctx, `UPDATE profiles SET resume_text = 'SRE intern at Acme 2024. Go, Kubernetes. (512) 555-0142' WHERE user_id = $1`, testUserID)

	page := "Menu Jobs\nAbout the role: keep our robot fleet online and fast. You will use Kubernetes and Go every day.\nApply"
	b, _ := json.Marshal(map[string]string{"url": "https://acme.example/jobs/1", "title": "SRE at Acme", "text": page + strings.Repeat(" More details about benefits.", 10)})
	code, body := do(t, client, http.MethodPost, ts.URL+"/api/ext/extract", string(b))
	if code != 200 || !strings.Contains(body, `"company":"Acme Robotics"`) || !strings.Contains(body, "About the role: keep our robot fleet online and fast. You will use Kubernetes and Go every day.") {
		t.Errorf("extract: %d %s", code, body)
	}
	if code, _ := do(t, client, http.MethodPost, ts.URL+"/api/ext/extract", `{"text":"too short"}`); code != 422 {
		t.Errorf("short page: want 422, got %d", code)
	}

	code, body = do(t, client, http.MethodPost, ts.URL+"/api/ext/answer", `{"question":"Why do you want to work at Acme?","company":"Acme","title":"SRE","description":"Robots"}`)
	if code != 200 || !strings.Contains(body, "keep robots online") || strings.Contains(body, "—") {
		t.Errorf("answer: %d %s", code, body)
	}
	if _, body = do(t, client, http.MethodPost, ts.URL+"/api/ext/answer", `{"question":"What are your salary expectations?"}`); !strings.Contains(body, `"needs_input"`) {
		t.Errorf("salary should ask the user: %s", body)
	}

	code, body = do(t, client, http.MethodPost, fmt.Sprintf("%s/api/jobs/%d/tailored-resume", ts.URL, testJobIDs[0]), "")
	if code != 200 || !strings.Contains(body, "Kept services healthy") || strings.Contains(body, `"bullets":["Kept services healthy","Cut costs by 87%"]`) || !strings.Contains(body, "87%") {
		t.Errorf("tailor (invented 87%% must move to dropped): %d %s", code, body)
	}

	_, body = do(t, client, http.MethodPost, ts.URL+"/api/applicant/suggest", "")
	if !strings.Contains(body, `"school":"UT Austin"`) || !strings.Contains(body, `"phone":"(512) 555-0142"`) {
		t.Errorf("suggest (phone must come from the resume): %s", body)
	}
}

func TestPasswordChangeDisconnectsExtensions(t *testing.T) {
	ts, pool := testServer(t, nil)
	ext, email := connectExtension(t, ts.URL)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })
	var uid int64
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&uid)
	hash, err := auth.HashPassword("a brand new password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPassword(context.Background(), pool, uid, hash, ""); err != nil {
		t.Fatal(err)
	}
	if code, _ := do(t, ext, http.MethodGet, ts.URL+"/api/auth/me", ""); code != 401 {
		t.Errorf("extension still connected after a password change: %d", code)
	}
}

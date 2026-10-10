package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"wera/internal/metrics"
	"wera/internal/store"
)

func TestPutApplicationPersists(t *testing.T) {
	ts, pool := testServer(t, nil)

	jobs, err := store.ListJobs(context.Background(), pool, testUserID, store.JobQuery{Limit: 1})
	if err != nil || len(jobs) == 0 {
		t.Skipf("no jobs available (err=%v)", err)
	}
	id := jobs[0].ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM applications WHERE job_id = $1 AND user_id = $2`, id, testUserID)
	})

	put := func(payload string) int {
		req, err := http.NewRequest(http.MethodPut, jobURL(ts, id, "/application"),
			strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("PUT %s: %d %s", payload, resp.StatusCode, body)
		}
		return resp.StatusCode
	}

	// Invalid status is rejected.
	req, _ := http.NewRequest(http.MethodPut, jobURL(ts, id, "/application"),
		strings.NewReader(`{"status":"nonsense"}`))
	r, _ := client.Do(req)
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("invalid status: want 400, got %d", r.StatusCode)
	}

	put(`{"status":"saved","notes":"api test"}`)
	put(`{"status":"applied","notes":"api test"}`)

	var appStatus string
	var appliedAt *string
	err = pool.QueryRow(context.Background(),
		`SELECT status, applied_at::text FROM applications WHERE job_id = $1 AND user_id = $2`, id, testUserID).
		Scan(&appStatus, &appliedAt)
	if err != nil {
		t.Fatalf("application row missing: %v", err)
	}
	if appStatus != "applied" {
		t.Errorf("status: got %q, want applied", appStatus)
	}
	if appliedAt == nil || *appliedAt == "" {
		t.Error("applied_at not set on first 'applied'")
	}
}

func TestSummaryEndpoints(t *testing.T) {
	ts, _ := testServer(t, nil)
	for _, path := range []string{
		"/api/today", "/api/stats", "/api/runs?limit=5", "/api/companies",
		"/api/usage", "/api/usage/me", "/api/today?limit=5", "/api/excluded?reason=title:senior", "/api/industries",
	} {
		code, body := getBody(t, ts.URL+path)
		if code != 200 {
			t.Errorf("GET %s: want 200, got %d (%s)", path, code, body)
		}
	}
}

func TestMetricsAndCORS(t *testing.T) {
	reg := metrics.New()
	reg.JobsNewTotal.Add(3)
	reg.FetchTotal.WithLabelValues("TestCo", "ok").Inc()
	reg.LLMRequests.WithLabelValues("test-model", "scored").Inc()
	reg.LLMTokens.WithLabelValues("prompt").Add(100)
	ts, _ := testServer(t, reg)

	code, body := getBody(t, ts.URL+"/metrics")
	if code != 200 {
		t.Errorf("/metrics: %d", code)
	}
	for _, want := range []string{
		"wera_jobs_new_total 3",
		"wera_fetch_total{company=\"TestCo\",result=\"ok\"} 1",
		"wera_llm_requests_total{model=\"test-model\",result=\"scored\"} 1",
		"wera_llm_tokens_total{type=\"prompt\"} 100",
		"wera_llm_cost_usd_total",
		"wera_run_last_success_timestamp",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q", want)
		}
	}

	// CORS: allowed origin reflected, others not.
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/jobs", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("preflight: status=%d allow-origin=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req2.Header.Set("Origin", "http://evil.example")
	resp2, _ := client.Do(req2)
	resp2.Body.Close()
	if resp2.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin got CORS headers")
	}
}

func TestAuthFlow(t *testing.T) {
	ts, pool := testServer(t, nil)
	anon := &http.Client{}

	post := func(c *http.Client, path, body string) *http.Response {
		t.Helper()
		resp, err := c.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	// Protected endpoints need a session.
	resp, err := anon.Get(ts.URL + "/api/jobs")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("anonymous /api/jobs: want 401, got %d", resp.StatusCode)
	}

	email := fmt.Sprintf("signup-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })

	if r := post(anon, "/api/auth/signup", `{"email":"`+email+`","password":"short"}`); r.StatusCode != 400 {
		t.Errorf("short password: want 400, got %d", r.StatusCode)
	}
	r := post(anon, "/api/auth/signup", `{"email":"`+email+`","password":"long enough pw","name":"T"}`)
	if r.StatusCode != 201 {
		t.Fatalf("signup: want 201, got %d", r.StatusCode)
	}
	var cookie string
	for _, c := range r.Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("session cookie flags: %+v", c)
			}
		}
	}
	if cookie == "" {
		t.Fatal("signup set no session cookie")
	}
	if r := post(anon, "/api/auth/signup", `{"email":"`+strings.ToUpper(email)+`","password":"long enough pw"}`); r.StatusCode != 409 {
		t.Errorf("duplicate email: want 409, got %d", r.StatusCode)
	}

	user := &http.Client{Transport: cookieTransport{cookie}}
	resp, err = user.Get(ts.URL + "/api/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), email) {
		t.Errorf("/api/auth/me: %d %s", resp.StatusCode, body)
	}
	if r := post(user, "/api/runs", ``); r.StatusCode != 403 {
		t.Errorf("non-admin POST /api/runs: want 403, got %d", r.StatusCode)
	}

	if r := post(anon, "/api/auth/login", `{"email":"`+email+`","password":"wrong password"}`); r.StatusCode != 401 {
		t.Errorf("wrong password: want 401, got %d", r.StatusCode)
	}
	if r := post(anon, "/api/auth/login", `{"email":"`+email+`","password":"long enough pw"}`); r.StatusCode != 200 {
		t.Errorf("login: want 200, got %d", r.StatusCode)
	}

	// Logout ends the session.
	if r := post(user, "/api/auth/logout", ``); r.StatusCode != 204 {
		t.Errorf("logout: want 204, got %d", r.StatusCode)
	}
	resp, _ = user.Get(ts.URL + "/api/auth/me")
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("after logout: want 401, got %d", resp.StatusCode)
	}

	// Cross-origin writes are refused.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, err = anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("cross-origin POST: want 403, got %d", resp.StatusCode)
	}
}

func TestProfileFlow(t *testing.T) {
	ts, _ := testServer(t, nil)

	code, body := getBody(t, ts.URL+"/api/profile/options")
	if code != 200 || !strings.Contains(body, `"data_science"`) || !strings.Contains(body, `"aerospace"`) {
		t.Fatalf("/api/profile/options: %d %.200s", code, body)
	}
	code, body = getBody(t, ts.URL+"/api/profile")
	if code != 200 || !strings.Contains(body, `"ready":false`) {
		t.Fatalf("empty profile: %d %s", code, body)
	}

	// Upload the sample resume PDF.
	pdf, err := os.ReadFile("../profile/testdata/sample-resume.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "resume.pdf")
	fw.Write(pdf)
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/profile/resume", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Example Airlines") {
		t.Fatalf("resume upload: %d %s", resp.StatusCode, b)
	}

	// Without Coral settings the AI endpoints answer 503, never a crash.
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/profile/suggest", nil)
	if resp, err := client.Do(req); err != nil || resp.StatusCode != 503 {
		t.Errorf("suggest without Coral: want 503, got %v %v", resp, err)
	}

	put := func(payload string) (int, string) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/profile", strings.NewReader(payload))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}
	if code, body := put(`{"markdown":"# P","preferences":{"role_families":["astrology"],"levels":["mid"]}}`); code != 400 {
		t.Errorf("unknown family: want 400, got %d %s", code, body)
	}
	before := matchCalls.Load()
	code, body = put(`{"markdown":"# Candidate Profile\nData scientist.","preferences":{"role_families":["data_science"],` +
		`"levels":["entry","mid"],"max_years_required":5,"us_only":true},"answers":{"industries":["aerospace"],"work_modes":["remote"]}}`)
	if code != 200 || !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"resume_chars"`) {
		t.Fatalf("save profile: %d %s", code, body)
	}
	if !strings.Contains(body, `"aerospace"`) {
		t.Errorf("answers not stored: %s", body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for matchCalls.Load() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if matchCalls.Load() == before {
		t.Error("saving a profile did not start matching")
	}
}

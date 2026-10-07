package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"wera/internal/metrics"
	"wera/internal/store"
)

func TestPutApplicationPersists(t *testing.T) {
	ts, pool := testServer(t, nil)

	jobs, err := store.ListJobs(context.Background(), pool, store.JobQuery{Limit: 1})
	if err != nil || len(jobs) == 0 {
		t.Skipf("no jobs available (err=%v)", err)
	}
	id := jobs[0].ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM applications WHERE job_id = $1`, id)
	})

	put := func(payload string) int {
		req, err := http.NewRequest(http.MethodPut, jobURL(ts, id, "/application"),
			strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
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
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Errorf("invalid status: want 400, got %d", r.StatusCode)
	}

	put(`{"status":"saved","notes":"api test"}`)
	put(`{"status":"applied","notes":"api test"}`)

	var appStatus string
	var appliedAt *string
	err = pool.QueryRow(context.Background(),
		`SELECT status, applied_at::text FROM applications WHERE job_id = $1`, id).
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
		"/api/usage", "/api/excluded?reason=title:senior",
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("preflight: status=%d allow-origin=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req2.Header.Set("Origin", "http://evil.example")
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin got CORS headers")
	}
}

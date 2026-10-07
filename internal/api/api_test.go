package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/metrics"
	"wera/internal/store"
)

// testPool connects to the development database (WERA_TEST_DATABASE_URL or
// the local default). Tests that need it skip when it is unreachable.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("WERA_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://wera:wera@localhost:5433/wera?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("no test database (%v)", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("test database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testServer(t *testing.T, reg *metrics.Registry) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testPool(t)
	srv := &Server{
		Pool:    pool,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: reg,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, pool
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(b)
}

func jobURL(ts *httptest.Server, id int64, suffix string) string {
	return ts.URL + "/api/jobs/" + strconv.FormatInt(id, 10) + suffix
}

func TestHealthzAndJobsEndpoints(t *testing.T) {
	ts, _ := testServer(t, nil)

	code, body := getBody(t, ts.URL+"/healthz")
	if code != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("healthz: %d %s", code, body)
	}

	code, body = getBody(t, ts.URL+"/api/jobs?limit=5")
	if code != 200 || !strings.Contains(body, `"jobs"`) {
		t.Errorf("/api/jobs: %d %s", code, body)
	}
	var list struct {
		Jobs []store.JobView `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode /api/jobs: %v", err)
	}
	if len(list.Jobs) == 0 {
		t.Skip("no jobs in test database to assert against")
	}
	job := list.Jobs[0]
	if job.ID == 0 || job.Title == "" || job.Company == "" {
		t.Errorf("first job looks wrong: %+v", job)
	}

	// Detail view, then a missing job.
	code, body = getBody(t, jobURL(ts, job.ID, ""))
	if code != 200 || !strings.Contains(body, job.Title) {
		t.Errorf("/api/jobs/{id}: %d %s", code, body)
	}
	if code, _ = getBody(t, ts.URL+"/api/jobs/99999999"); code != 404 {
		t.Errorf("missing job want 404, got %d", code)
	}
}

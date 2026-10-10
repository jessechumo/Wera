package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/auth"
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

// client sends the test user's session cookie; testServer sets it up.
var client = http.DefaultClient

// testUserID is the throwaway user testServer logged client in as.
var testUserID int64

// cookieTransport adds a fixed session cookie to every request.
type cookieTransport struct{ cookie string }

func (c cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: c.cookie})
	return http.DefaultTransport.RoundTrip(r)
}

// testServer starts the API and logs `client` in as a throwaway admin
// user, deleted again when the test ends.
func testServer(t *testing.T, reg *metrics.Registry) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testPool(t)
	srv := &Server{
		Pool:          pool,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:       reg,
		SignupEnabled: true,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx := context.Background()
	email := fmt.Sprintf("api-test-%d@example.com", time.Now().UnixNano())
	u, err := store.CreateUser(ctx, pool, email, "API test", "", true)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID) })
	testUserID = u.ID
	// A few open jobs count as the user's matches, so job endpoints have data.
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_jobs (user_id, job_id, content_hash, stage)
		SELECT $1, id, content_hash, 'scored' FROM jobs
		WHERE closed_at IS NULL ORDER BY id LIMIT 5`, u.ID); err != nil {
		t.Fatalf("seed test matches: %v", err)
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, pool, u.ID, hash, time.Hour); err != nil {
		t.Fatal(err)
	}
	client = &http.Client{Transport: cookieTransport{token}}
	t.Cleanup(func() { client = http.DefaultClient })
	return ts, pool
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := client.Get(url)
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

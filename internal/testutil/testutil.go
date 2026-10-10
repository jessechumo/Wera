// Package testutil gives integration tests a PostgreSQL database and
// self-contained fixtures (users, companies, jobs) that clean up after
// themselves, so tests never depend on whatever data a database holds.
package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/scoring"
	"wera/internal/store"
)

// Pool connects to WERA_TEST_DATABASE_URL. Without it the test is skipped
// locally and fails in CI (CI must never silently skip the database
// tests). It refuses a database named "wera": that is the live one.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("WERA_TEST_DATABASE_URL")
	if raw == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("WERA_TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("WERA_TEST_DATABASE_URL not set; skipping database test")
	}
	if u, err := url.Parse(raw); err == nil && strings.TrimPrefix(u.Path, "/") == "wera" {
		t.Fatal("refusing to run tests against the live database \"wera\"; use a copy such as wera_dev or wera_test")
	}
	pool, err := pgxpool.New(context.Background(), raw)
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("test database unreachable: %v", err)
		}
		t.Skipf("test database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

var seq atomic.Int64

// Unique returns a name unique to this test process.
func Unique(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), seq.Add(1))
}

// User creates a user (deleted with everything they own at cleanup).
func User(t *testing.T, pool *pgxpool.Pool, admin bool) *store.User {
	t.Helper()
	u, err := store.CreateUser(context.Background(), pool, Unique("user")+"@example.com", "Test User", "", admin)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID) })
	return u
}

// Profile saves a profile for the user.
func Profile(t *testing.T, pool *pgxpool.Pool, userID int64, markdown string, prefs config.Preferences) *store.Profile {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveProfile(ctx, pool, userID, markdown, scoring.ProfileHash([]byte(markdown)), prefs, nil); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	p, err := store.GetProfile(ctx, pool, userID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	return p
}

// Company creates a company; its jobs (and their analyses) go at cleanup.
func Company(t *testing.T, pool *pgxpool.Pool, industry string) (id int64, name string) {
	t.Helper()
	name = Unique("Company")
	err := pool.QueryRow(context.Background(), `
		INSERT INTO companies (name, ats, token, industry) VALUES ($1, 'greenhouse', $1, $2) RETURNING id`,
		name, industry).Scan(&id)
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM analyses WHERE job_id IN (SELECT id FROM jobs WHERE company_id = $1)`, id)
		pool.Exec(ctx, `DELETE FROM jobs WHERE company_id = $1`, id)
		pool.Exec(ctx, `DELETE FROM companies WHERE id = $1`, id)
	})
	return id, name
}

// Job is a posting to insert with Jobs.
type Job struct {
	Title, Location, Description string
}

// Jobs inserts open postings for a company and returns their ids in order.
func Jobs(t *testing.T, pool *pgxpool.Pool, companyID int64, jobs ...Job) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(jobs))
	for _, j := range jobs {
		ext := Unique("ext")
		sum := sha256.Sum256([]byte(j.Title + j.Location + j.Description))
		var id int64
		err := pool.QueryRow(context.Background(), `
			INSERT INTO jobs (company_id, source, ext_id, title, location_raw, url, description, content_hash)
			VALUES ($1, 'greenhouse', $2, $3, $4, $5, $6, $7) RETURNING id`,
			companyID, ext, j.Title, j.Location, "https://example.com/jobs/"+ext, j.Description,
			hex.EncodeToString(sum[:])).Scan(&id)
		if err != nil {
			t.Fatalf("insert job: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

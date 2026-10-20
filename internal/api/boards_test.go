package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"wera/internal/pipeline"
	"wera/internal/store"
	"wera/internal/testutil"
)

func TestAddedJobBoardIsWatched(t *testing.T) {
	ctx := context.Background()
	slug := strings.ToLower(strings.ReplaceAll(testutil.Unique("board"), "-", ""))
	empty := slug + "empty"
	ts, pool := testServerWith(t, nil, func(s *Server) {
		s.CheckBoard = func(_ context.Context, ats, token string) (int, error) {
			if ats == "greenhouse" && token == slug {
				return 3, nil
			}
			return 0, nil
		}
	})
	ext, email := connectExtension(t, ts.URL)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM user_jobs WHERE job_id IN (SELECT j.id FROM jobs j JOIN companies c ON c.id = j.company_id WHERE c.name LIKE 'Board Co %')`)
		pool.Exec(ctx, `DELETE FROM jobs WHERE company_id IN (SELECT id FROM companies WHERE name LIKE 'Board Co %')`)
		pool.Exec(ctx, `DELETE FROM companies WHERE name LIKE 'Board Co %'`)
		pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	})

	add := func(company, url string) {
		t.Helper()
		body := fmt.Sprintf(`{"url":%q,"company":%q,"title":"Charge Nurse","industry":"hospitals"}`, url, company)
		if code, out := do(t, ext, http.MethodPost, ts.URL+"/api/ext/jobs", body); code != 201 {
			t.Fatalf("add job: %d %s", code, out)
		}
	}
	add("Board Co "+slug, "https://job-boards.greenhouse.io/"+slug+"/jobs/42")
	add("Board Co "+empty, "https://boards.greenhouse.io/"+empty+"/jobs/43")
	add("Board Co "+slug+" site", "https://careers.example.com/"+slug+"/44")

	var ats, token, industry string
	var enabled bool
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := pool.QueryRow(ctx, `SELECT ats, token, industry, enabled FROM companies WHERE name = $1 AND added_at IS NOT NULL`,
			"Board Co "+slug).Scan(&ats, &token, &industry, &enabled)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the added job's board was never watched: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ats != "greenhouse" || token != slug || industry != "hospitals" || !enabled {
		t.Errorf("watched board = %s/%s (%s, enabled %v)", ats, token, industry, enabled)
	}

	added, err := store.AddedCompanies(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(added))
	for _, c := range added {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "Board Co "+slug) || strings.Contains(got, empty) || strings.Contains(got, " site") {
		t.Errorf("added boards = %s; want only the board that has jobs", got)
	}

	// A company the YAML list already has is not fetched twice, and the
	// same board added again is not watched again.
	both, err := pipeline.WithAdded(ctx, pool, added[:0])
	if err != nil || len(both) != len(added) {
		t.Errorf("WithAdded = %d companies (%v), want %d", len(both), err, len(added))
	}
	if can, err := store.CanWatchBoard(ctx, pool, 0, "greenhouse", strings.ToUpper(slug)); err != nil || can {
		t.Errorf("a watched board can be watched again: %v %v", can, err)
	}
}

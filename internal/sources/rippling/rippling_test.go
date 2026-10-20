package rippling

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"wera/internal/sources"
)

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v2/board/acme-careers/jobs" && r.URL.Query().Get("page") == "0":
			serve(t, w, "testdata/page0.json")
		case r.URL.Path == "/api/v2/board/acme-careers/jobs" && r.URL.Query().Get("page") == "1":
			serve(t, w, "testdata/page1.json")
		case r.URL.Path == "/api/v2/board/acme-careers/jobs/6e08038a-0002":
			serve(t, w, "testdata/detail.json")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL(sources.NewHTTP("Wera/test"), srv.URL)
}

func serve(t *testing.T, w http.ResponseWriter, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func TestListAndDetail(t *testing.T) {
	a := newTestAdapter(t)
	jobs, err := a.List(context.Background(), "acme-careers")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("want 3 jobs over two pages (one listed twice), got %d", len(jobs))
	}
	if j := jobs[0]; j.Title != "Staff Accountant" || j.LocationRaw != "Remote (United States)" || !*j.IsRemote || j.Department != "Accounting & Finance" {
		t.Errorf("remote job: %+v", j)
	}
	j := jobs[1]
	if j.URL != "https://ats.rippling.com/acme-careers/jobs/6e08038a-0002" || !*j.IsRemote ||
		j.LocationRaw != "Hybrid (Castle Rock, Colorado, US) (United States); Denver, CO (United States); Boulder, CO (United States)" {
		t.Errorf("listed job: %+v", j)
	}
	if err := a.Detail(context.Background(), "acme-careers", &j); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if j.Title != "Charge Nurse (Days)" || !strings.Contains(j.DescriptionText, "30-bed unit") || !strings.Contains(j.DescriptionText, "About Acme Health") {
		t.Errorf("detailed job: %q %q", j.Title, j.DescriptionText)
	}
	if j.PostedAt == nil || j.PostedAt.UTC().Format("2006-01-02") != "2026-09-28" {
		t.Errorf("posted: %v", j.PostedAt)
	}
}

func TestUnknownBoard(t *testing.T) {
	if _, err := newTestAdapter(t).List(context.Background(), "nobody"); !errors.Is(err, sources.ErrBoardNotFound) {
		t.Errorf("want ErrBoardNotFound, got %v", err)
	}
}

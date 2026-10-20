package workable

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"wera/internal/sources"
)

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/widget/accounts/acme":
			serve(t, w, "testdata/widget.json")
		case "/api/v2/accounts/acme/jobs/AB12CD34EF":
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
	jobs, err := a.List(context.Background(), "acme")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs (one without a shortcode and a repeat skipped), got %d", len(jobs))
	}
	j := jobs[0]
	if j.ExtID != "AB12CD34EF" || j.Title != "Registered Nurse - ICU" || j.LocationRaw != "Austin, Texas, United States" ||
		j.Department != "Nursing" || j.URL != "https://apply.workable.com/j/AB12CD34EF" || *j.IsRemote {
		t.Errorf("listed job: %+v", j)
	}
	if j.PostedAt == nil || !j.PostedAt.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("posted: %v", j.PostedAt)
	}
	if r := jobs[1]; r.URL != "https://apply.workable.com/acme/j/ZZ99YY88XX" || !*r.IsRemote || r.LocationRaw != "United States" {
		t.Errorf("remote job: %+v", r)
	}

	if err := a.Detail(context.Background(), "acme", &j); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if j.Title != "Registered Nurse - ICU (Nights)" {
		t.Errorf("title: %q", j.Title)
	}
	if !strings.Contains(j.DescriptionText, "critically ill") || !strings.Contains(j.DescriptionText, "Texas RN license") {
		t.Errorf("description: %q", j.DescriptionText)
	}
}

func TestUnknownAccount(t *testing.T) {
	_, err := newTestAdapter(t).List(context.Background(), "nobody")
	if !errors.Is(err, sources.ErrBoardNotFound) {
		t.Errorf("want ErrBoardNotFound, got %v", err)
	}
}

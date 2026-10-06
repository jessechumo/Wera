package lever

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"wera/internal/sources"
)

func newTestAdapter(t *testing.T) (*Adapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/postings/testco" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("mode") != "json" {
			http.Error(w, "want mode=json", http.StatusBadRequest)
			return
		}
		data, err := os.ReadFile("testdata/board.json")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL(sources.NewHTTP("Wera/test"), srv.URL), srv
}

func TestFetchParsesFixture(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	jobs, err := adapter.Fetch(context.Background(), "testco")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("want 3 jobs, got %d", len(jobs))
	}

	// Job 0: full posting with lists and HTML description.
	j0 := jobs[0]
	if j0.ExtID != "a1b2c3d4-0001" || j0.Title != "Trading Systems Engineer" {
		t.Errorf("job0 id/title: got %q/%q", j0.ExtID, j0.Title)
	}
	if j0.LocationRaw != "Chicago, IL (Hybrid)" {
		t.Errorf("job0 location: got %q", j0.LocationRaw)
	}
	if j0.Department != "Technology" {
		t.Errorf("job0 department: got %q", j0.Department)
	}
	if j0.URL != "https://jobs.lever.co/belvederetrading/a1b2c3d4-0001" {
		t.Errorf("job0 url: got %q", j0.URL)
	}
	if j0.IsRemote != nil {
		t.Errorf("job0 is_remote: want nil for hybrid, got %v", *j0.IsRemote)
	}
	wantPosted := time.UnixMilli(1757083800000).UTC()
	if j0.PostedAt == nil || !j0.PostedAt.Equal(wantPosted) {
		t.Errorf("job0 posted_at: got %v, want %v", j0.PostedAt, wantPosted)
	}
	wantText := "Support our trading infrastructure. We do not sponsor visas.\n\n" +
		"What you'll do\nRun Linux\nAutomate deployments\n\n" +
		"Requirements\n2+ years of experience\n\n" +
		"Belvedere is an equal opportunity employer."
	if j0.DescriptionText != wantText {
		t.Errorf("job0 description text:\n got: %q\nwant: %q", j0.DescriptionText, wantText)
	}

	// Job 2: remote via workplaceType.
	j2 := jobs[2]
	if j2.IsRemote == nil || !*j2.IsRemote {
		t.Errorf("job2 is_remote: want true, got %v", j2.IsRemote)
	}

	// Job 1: plain-only description, no lists.
	j1 := jobs[1]
	if j1.DescriptionText != "Research strategies." {
		t.Errorf("job1 description text: got %q", j1.DescriptionText)
	}
	if j1.DescriptionHTML != "" {
		t.Errorf("job1 description html: want empty, got %q", j1.DescriptionHTML)
	}
}

func TestFetchBoardNotFound(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	_, err := adapter.Fetch(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for unknown board")
	}
}

func TestParseTimestamp(t *testing.T) {
	if got := ParseTimestamp(0); got != nil {
		t.Errorf("ParseTimestamp(0): got %v, want nil", got)
	}
	if got := ParseTimestamp(-5); got != nil {
		t.Errorf("ParseTimestamp(-5): got %v, want nil", got)
	}
	want := time.UnixMilli(1757083800000).UTC()
	got := ParseTimestamp(1757083800000)
	if got == nil || !got.Equal(want) {
		t.Errorf("ParseTimestamp: got %v, want %v", got, want)
	}
}

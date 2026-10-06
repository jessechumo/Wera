package ashby

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
		if r.URL.Path != "/posting-api/job-board/testco" {
			http.NotFound(w, r)
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
	// The isListed=false posting must be skipped.
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs (isListed=false skipped), got %d", len(jobs))
	}

	// Job 0: multi-location onsite posting.
	j0 := jobs[0]
	if j0.ExtID != "f7e6d5c4-0001" || j0.Title != "Infrastructure Engineer" {
		t.Errorf("job0 id/title: got %q/%q", j0.ExtID, j0.Title)
	}
	if j0.LocationRaw != "San Francisco, New York, Austin" {
		t.Errorf("job0 location: got %q", j0.LocationRaw)
	}
	if j0.Department != "EPD" {
		t.Errorf("job0 department: got %q", j0.Department)
	}
	if j0.URL != "https://jobs.ashbyhq.com/baseten/f7e6d5c4-0001" {
		t.Errorf("job0 url: got %q", j0.URL)
	}
	if j0.IsRemote == nil || *j0.IsRemote {
		t.Errorf("job0 is_remote: want false, got %v", j0.IsRemote)
	}
	wantPosted := time.Date(2026, 9, 5, 9, 30, 0, 0, time.UTC)
	if j0.PostedAt == nil || !j0.PostedAt.Equal(wantPosted) {
		t.Errorf("job0 posted_at: got %v, want %v", j0.PostedAt, wantPosted)
	}
	if j0.DescriptionText != "Own our Kubernetes fleet." {
		t.Errorf("job0 description text: got %q", j0.DescriptionText)
	}

	// Job 1: remote posting with no HTML description.
	j1 := jobs[1]
	if j1.IsRemote == nil || !*j1.IsRemote {
		t.Errorf("job1 is_remote: want true, got %v", j1.IsRemote)
	}
	if j1.DescriptionText != "Build the training platform.\n\nWe sponsor visas." {
		t.Errorf("job1 description text: got %q", j1.DescriptionText)
	}
	if j1.DescriptionHTML == "" {
		t.Errorf("job1 description html: want derived from plain text, got empty")
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
	if got := ParseTimestamp(""); got != nil {
		t.Errorf("ParseTimestamp(\"\"): got %v, want nil", got)
	}
	if got := ParseTimestamp("not-a-date"); got != nil {
		t.Errorf("ParseTimestamp(bad): got %v, want nil", got)
	}
	want := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	got := ParseTimestamp("2026-09-10T14:00:00.000Z")
	if got == nil || !got.Equal(want) {
		t.Errorf("ParseTimestamp: got %v, want %v", got, want)
	}
}

func TestJoinLocations(t *testing.T) {
	loc := func(s string) []struct {
		Location string `json:"location"`
	} {
		if s == "" {
			return nil
		}
		return []struct {
			Location string `json:"location"`
		}{{Location: s}}
	}
	if got := joinLocations("Remote", nil); got != "Remote" {
		t.Errorf("joinLocations: got %q", got)
	}
	if got := joinLocations("", loc("NYC")); got != "NYC" {
		t.Errorf("joinLocations: got %q", got)
	}
	if got := joinLocations("  ", nil); got != "" {
		t.Errorf("joinLocations: got %q", got)
	}
}

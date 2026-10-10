package greenhouse

import (
	"context"
	"errors"
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
		if r.URL.Path != "/v1/boards/testco/jobs" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `W/"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		data, err := os.ReadFile("testdata/board.json")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `W/"v1"`)
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

	tests := []struct {
		name string
		got  sources.RawJob
		want sources.RawJob
	}{
		{
			name: "sre job with html content",
			got:  jobs[0],
			want: sources.RawJob{
				ExtID:       "4019309",
				Title:       "Site Reliability Engineer",
				LocationRaw: "Chicago, IL, United States",
				URL:         "https://job-boards.greenhouse.io/drweng/jobs/4019309",
				Department:  "Technology",
				DescriptionHTML: "<p>We are looking for an <strong>SRE</strong> in Chicago.&nbsp;You will run Linux.&nbsp;</p>" +
					"<ul><li>Linux</li><li>Kubernetes</li></ul><p>We do not sponsor visas.</p>",
				DescriptionText: "We are looking for an SRE in Chicago. You will run Linux.\n\nLinux\nKubernetes\n\nWe do not sponsor visas.",
				PostedAt: func() *time.Time {
					tt, _ := time.Parse(time.RFC3339, "2026-09-01T12:00:00Z")
					return &tt
				}(),
			},
		},
		{
			name: "intern job without departments",
			got:  jobs[1],
			want: sources.RawJob{
				ExtID:           "4019310",
				Title:           "Software Engineer Intern",
				LocationRaw:     "New York, NY",
				URL:             "https://job-boards.greenhouse.io/drweng/jobs/4019310",
				Department:      "",
				DescriptionHTML: "<p>Join our <em>summer</em> internship program &amp; have fun.</p>",
				DescriptionText: "Join our summer internship program & have fun.",
				PostedAt:        mustTime(t, "2026-09-05T09:30:00Z"),
			},
		},
		{
			name: "remote trading job with offset timestamp",
			got:  jobs[2],
			want: sources.RawJob{
				ExtID:           "4019311",
				Title:           "Trade Systems Engineer",
				LocationRaw:     "Remote - US",
				URL:             "https://job-boards.greenhouse.io/drweng/jobs/4019311",
				Department:      "Trading",
				DescriptionHTML: "<p>Support trading systems.</p>",
				DescriptionText: "Support trading systems.",
				PostedAt:        mustTime(t, "2026-09-10T08:00:00-05:00"),
				IsRemote:        boolPtr(true),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, want := tc.got, tc.want
			if got.ExtID != want.ExtID {
				t.Errorf("ExtID: got %q, want %q", got.ExtID, want.ExtID)
			}
			if got.Title != want.Title {
				t.Errorf("Title: got %q, want %q", got.Title, want.Title)
			}
			if got.LocationRaw != want.LocationRaw {
				t.Errorf("LocationRaw: got %q, want %q", got.LocationRaw, want.LocationRaw)
			}
			if got.URL != want.URL {
				t.Errorf("URL: got %q, want %q", got.URL, want.URL)
			}
			if got.Department != want.Department {
				t.Errorf("Department: got %q, want %q", got.Department, want.Department)
			}
			if got.DescriptionHTML != want.DescriptionHTML {
				t.Errorf("DescriptionHTML: got %q, want %q", got.DescriptionHTML, want.DescriptionHTML)
			}
			if got.DescriptionText != want.DescriptionText {
				t.Errorf("DescriptionText: got %q, want %q", got.DescriptionText, want.DescriptionText)
			}
			if want.PostedAt == nil {
				if got.PostedAt != nil {
					t.Errorf("PostedAt: got %v, want nil", got.PostedAt)
				}
			} else if got.PostedAt == nil || !got.PostedAt.Equal(*want.PostedAt) {
				t.Errorf("PostedAt: got %v, want %v", got.PostedAt, want.PostedAt)
			}
			if (got.IsRemote == nil) != (want.IsRemote == nil) {
				t.Errorf("IsRemote: got %v, want %v", got.IsRemote, want.IsRemote)
			} else if got.IsRemote != nil && *got.IsRemote != *want.IsRemote {
				t.Errorf("IsRemote: got %v, want %v", *got.IsRemote, *want.IsRemote)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestFetchBoardNotFound(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	_, err := adapter.Fetch(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for unknown board")
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		in   string
		want *time.Time
	}{
		{"2026-09-01T12:00:00Z", mustTime(t, "2026-09-01T12:00:00Z")},
		{"2026-09-10T08:00:00-05:00", mustTime(t, "2026-09-10T08:00:00-05:00")},
		{"2026-09-01T12:00:00", mustTime(t, "2026-09-01T12:00:00Z")},
		{"2026-09-01", mustTime(t, "2026-09-01T00:00:00Z")}, // date only: parsed as midnight UTC
		{"", nil},
		{"not-a-date", nil},
	}
	for _, tc := range tests {
		got := ParseTimestamp(tc.in)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("ParseTimestamp(%q): got %v, want nil", tc.in, got)
		case tc.want != nil && (got == nil || !got.Equal(*tc.want)):
			t.Errorf("ParseTimestamp(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}

func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &tt
}

func TestFetchIfChanged(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	jobs, etag, err := adapter.FetchIfChanged(context.Background(), "testco", "")
	if err != nil || len(jobs) != 3 || etag != `W/"v1"` {
		t.Fatalf("first fetch: %d jobs, etag %q, %v", len(jobs), etag, err)
	}
	jobs, etag, err = adapter.FetchIfChanged(context.Background(), "testco", etag)
	if !errors.Is(err, sources.ErrNotModified) || jobs != nil || etag != `W/"v1"` {
		t.Fatalf("unchanged board: want ErrNotModified, got %d jobs, etag %q, %v", len(jobs), etag, err)
	}
}

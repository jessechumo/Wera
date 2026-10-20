package oracle

import (
	"context"
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
		finder := r.URL.Query().Get("finder")
		switch r.URL.Path {
		case "/hcmRestApi/resources/latest/recruitingCEJobRequisitions":
			if !strings.HasPrefix(finder, "findReqs;siteNumber=CX_1,") || !strings.Contains(finder, "sortBy=POSTING_DATES_DESC") {
				t.Errorf("bad list finder %q", finder)
			}
			serve(t, w, "testdata/list.json")
		case "/hcmRestApi/resources/latest/recruitingCEJobRequisitionDetails":
			if finder != `ById;Id="400067771",siteNumber=CX_1` {
				w.Write([]byte(`{"items":[],"count":0}`))
				return
			}
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
	jobs, complete, err := a.ListNew(context.Background(), "acme.fa.us2/CX_1", map[string]bool{})
	if err != nil || !complete {
		t.Fatalf("ListNew: %v (complete %v)", err, complete)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(jobs))
	}
	j := jobs[0]
	if j.ExtID != "400067771" || j.Title != "Registered Nurse - Med/Surg" ||
		j.LocationRaw != "Columbus, OH, United States; Dublin, OH, United States" || *j.IsRemote ||
		j.URL != "https://acme.fa.us2.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1/job/400067771" {
		t.Errorf("listed job: %+v", j)
	}
	if !*jobs[1].IsRemote {
		t.Errorf("remote job not remote: %+v", jobs[1])
	}
	if err := a.Detail(context.Background(), "acme.fa.us2/CX_1", &j); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if j.Title != "Registered Nurse - Med/Surg (Nights)" || !strings.Contains(j.DescriptionText, "32-bed") ||
		!strings.Contains(j.DescriptionText, "Assess and monitor") || !strings.Contains(j.DescriptionText, "Ohio RN license") {
		t.Errorf("detailed job: %q %q", j.Title, j.DescriptionText)
	}
	if j.PostedAt == nil || !j.PostedAt.Equal(time.Date(2026, 10, 20, 13, 7, 44, 0, time.UTC)) {
		t.Errorf("posted: %v", j.PostedAt)
	}
	gone := jobs[1]
	if err := a.Detail(context.Background(), "acme.fa.us2/CX_1", &gone); err == nil {
		t.Error("a posting the site no longer has must fail its detail")
	}
}

func TestParseToken(t *testing.T) {
	if s, err := parseToken("jpmc.fa/CX_1001"); err != nil || s.host != "https://jpmc.fa.oraclecloud.com" || s.number != "CX_1001" {
		t.Errorf("parseToken = %+v, %v", s, err)
	}
	for _, bad := range []string{"jpmc.fa", "/CX", "jpmc.fa/", "https://x/CX"} {
		if _, err := parseToken(bad); err == nil {
			t.Errorf("parseToken(%q) should fail", bad)
		}
	}
}

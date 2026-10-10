package amazon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wera/internal/sources"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/en/search.json" || q.Get("country") != "USA" || q.Get("sort") != "recent" {
			http.NotFound(w, r)
			return
		}
		// 150 hits: page 0 has ids 1-100, page 1 has 101-150.
		var jobs []string
		start := 1
		if q.Get("offset") == "100" {
			start = 101
		}
		for id := start; id < start+100 && id <= 150; id++ {
			jobs = append(jobs, fmt.Sprintf(`{"id_icims":"%d","title":"SDE %d","normalized_location":"Seattle, Washington, USA",`+
				`"job_category":"Software Development","job_path":"/en/jobs/%d/sde","posted_date":"October 10, 2026",`+
				`"description":"Build AWS.","basic_qualifications":"- 3+ years<br/>- Java","preferred_qualifications":""}`, id, id, id))
		}
		fmt.Fprintf(w, `{"hits":150,"jobs":[%s]}`, strings.Join(jobs, ","))
	}))
	t.Cleanup(srv.Close)

	jobs, err := NewWithBaseURL(sources.NewHTTP("Wera/test"), srv.URL).Fetch(context.Background(), "USA")
	if err != nil || len(jobs) != 150 {
		t.Fatalf("Fetch: %d jobs, %v", len(jobs), err)
	}
	j := jobs[149]
	if j.ExtID != "150" || j.URL != srv.URL+"/en/jobs/150/sde" || j.LocationRaw != "Seattle, Washington, USA" {
		t.Errorf("job: %+v", j)
	}
	if !strings.Contains(j.DescriptionText, "Build AWS.") || !strings.Contains(j.DescriptionText, "3+ years") ||
		strings.Contains(j.DescriptionText, "Preferred") {
		t.Errorf("description: %q", j.DescriptionText)
	}
	if j.PostedAt == nil || j.PostedAt.Day() != 10 {
		t.Errorf("posted: %v", j.PostedAt)
	}
}

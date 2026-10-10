package smartrecruiters

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
		file := ""
		switch r.URL.Path {
		case "/v1/companies/TestCo/postings":
			file = "testdata/list.json"
		case "/v1/companies/TestCo/postings/111":
			file = "testdata/detail.json"
		case "/v1/companies/Nobody/postings":
			w.Write([]byte(`{"totalFound":0,"content":[]}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL(sources.NewHTTP("Wera/test"), srv.URL)
}

func TestListAndDetail(t *testing.T) {
	a := newTestAdapter(t)
	jobs, err := a.List(context.Background(), "TestCo")
	if err != nil || len(jobs) != 2 {
		t.Fatalf("List: %d jobs, %v", len(jobs), err)
	}
	if jobs[0].Title != "Software Engineer" || jobs[0].LocationRaw != "Santa Clara, California, United States" || jobs[0].PostedAt == nil {
		t.Errorf("job0: %+v", jobs[0])
	}
	if jobs[1].LocationRaw != "Austin, Texas, US (Remote)" || !*jobs[1].IsRemote || jobs[1].PostedAt != nil {
		t.Errorf("job1: %+v", jobs[1])
	}
	j := jobs[0]
	if err := a.Detail(context.Background(), "TestCo", &j); err != nil {
		t.Fatal(err)
	}
	want := "We make software."
	if !strings.HasPrefix(j.DescriptionText, want) || !strings.Contains(j.DescriptionText, "Build services.") ||
		strings.Index(j.DescriptionText, "Build services.") > strings.Index(j.DescriptionText, "Go") {
		t.Errorf("sections out of order: %q", j.DescriptionText)
	}
	if j.URL != "https://jobs.smartrecruiters.com/TestCo/111-software-engineer" {
		t.Errorf("url: %q", j.URL)
	}
	if _, err := a.List(context.Background(), "Nobody"); !errors.Is(err, sources.ErrBoardNotFound) {
		t.Errorf("empty company: want ErrBoardNotFound, got %v", err)
	}
}

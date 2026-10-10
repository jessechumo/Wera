package workday

import (
	"context"
	"encoding/json"
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
		const base = "/wday/cxs/acme/Careers"
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base+"/jobs":
			var req searchRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Limit != pageSize {
				t.Errorf("bad search request: %+v %v", req, err)
			}
			if req.Offset > 0 {
				w.Write([]byte(`{"total":0,"jobPostings":[]}`))
				return
			}
			serve(t, w, "testdata/page0.json")
		case r.Method == http.MethodGet && r.URL.Path == base+"/job/US-TX-Dallas/Data-Scientist_JR2":
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
	jobs, err := a.List(context.Background(), "acme.wd5/Careers")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 deduplicated jobs, got %d", len(jobs))
	}
	j := jobs[1]
	if j.ExtID != "/job/US-TX-Dallas/Data-Scientist_JR2" ||
		j.URL != "https://acme.wd5.myworkdayjobs.com/Careers/job/US-TX-Dallas/Data-Scientist_JR2" {
		t.Errorf("listed job: %+v", j)
	}
	if err := a.Detail(context.Background(), "acme.wd5/Careers", &j); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if j.Title != "Data Scientist, Flight Operations" {
		t.Errorf("title: %q", j.Title)
	}
	if j.LocationRaw != "US, TX, Dallas; US, Remote (United States of America)" {
		t.Errorf("location: %q", j.LocationRaw)
	}
	if !strings.Contains(j.DescriptionText, "Build models for flight delays") {
		t.Errorf("description: %q", j.DescriptionText)
	}
	if j.PostedAt == nil || !j.PostedAt.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("posted: %v", j.PostedAt)
	}
	if j.IsRemote == nil || !*j.IsRemote {
		t.Error("want remote")
	}
}

func TestParseToken(t *testing.T) {
	s, err := parseToken("nvidia.wd5/NVIDIAExternalCareerSite")
	if err != nil || s.tenant != "nvidia" || s.host != "https://nvidia.wd5.myworkdayjobs.com" || s.name != "NVIDIAExternalCareerSite" {
		t.Errorf("parse: %+v %v", s, err)
	}
	for _, bad := range []string{"nvidia", "nvidia/site", "nvidia.wd5/", ".wd5/x"} {
		if _, err := parseToken(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

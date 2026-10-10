package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestSponsorship(t *testing.T) {
	ts, _ := testServer(t, nil)
	code, out := do(t, client, http.MethodGet, ts.URL+"/api/sponsorship?limit=5", "")
	if code != 200 || !strings.Contains(out, `"companies":`) {
		t.Fatalf("sponsorship: %d %s", code, out)
	}
}

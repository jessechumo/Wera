package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"wera/internal/store"
)

func TestSponsorship(t *testing.T) {
	ts, _ := testServer(t, nil)
	code, out := do(t, client, http.MethodGet, ts.URL+"/api/sponsorship?limit=5", "")
	if code != 200 || !strings.Contains(out, `"companies":`) || !strings.Contains(out, `"summary":`) {
		t.Fatalf("sponsorship: %d %s", code, out)
	}
}

func TestSummarize(t *testing.T) {
	n := int64(3)
	zero := int64(0)
	s := summarize([]store.SponsorshipStat{
		{Signal: "sponsors", H1BFilings: &n}, {Signal: "mixed"}, {Signal: "does_not_sponsor", H1BFilings: &zero}, {Signal: "unclear"},
	})
	want := sponsorSummary{Companies: 4, Filers: 1, Sponsors: 1, Mixed: 1, DoesNotSponsor: 1}
	if s != want {
		t.Errorf("summary: %+v, want %+v", s, want)
	}
	raw, _ := json.Marshal(s)
	if !strings.Contains(string(raw), `"does_not_sponsor":1`) {
		t.Errorf("json: %s", raw)
	}
}

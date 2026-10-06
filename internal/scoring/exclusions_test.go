package scoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScorerPostLLMExclusions(t *testing.T) {
	tests := []struct {
		name     string
		reply    string
		stage    string
		reason   string
		evidence string
	}{
		{"sponsorship refusal", `{"fit_score":90,"verdict":"strong","seniority":"junior","years_required":null,"sponsorship":"no","sponsorship_quote":"We do not sponsor visas.","us_eligible":true,"work_mode":"onsite","location_summary":"NYC","skills_matched":[],"skills_missing":[],"reason":"Good match."}`,
			StageExcluded, "llm:sponsorship_no", "We do not sponsor visas."},
		{"years over max", `{"fit_score":90,"verdict":"strong","seniority":"mid","years_required":6,"sponsorship":"unknown","sponsorship_quote":null,"us_eligible":true,"work_mode":"remote","location_summary":"Remote","skills_matched":[],"skills_missing":[],"reason":"Needs 6 years."}`,
			StageExcluded, "llm:years>3", "Needs 6 years."},
		{"non us", `{"fit_score":40,"verdict":"stretch","seniority":"junior","years_required":1,"sponsorship":"unknown","sponsorship_quote":null,"us_eligible":false,"work_mode":"onsite","location_summary":"London, UK","skills_matched":[],"skills_missing":[],"reason":"Based in London."}`,
			StageExcluded, "llm:non_us", "London, UK"},
		{"senior seniority", `{"fit_score":60,"verdict":"stretch","seniority":"senior","years_required":null,"sponsorship":"unknown","sponsorship_quote":null,"us_eligible":true,"work_mode":"remote","location_summary":"Remote","skills_matched":[],"skills_missing":[],"reason":"Senior role."}`,
			StageExcluded, "llm:senior", "Senior role."},
	}

	// Score one job at a time so each scripted reply maps to its job
	// deterministically (Score runs jobs concurrently).
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestScorer(t, tc.reply)
			outs := s.Score(context.Background(), testJobs(1))
			o := outs[0]
			if o.Stage != tc.stage || o.ExcludeReason != tc.reason || o.ExcludeEvidence != tc.evidence {
				t.Errorf("got stage=%q reason=%q evidence=%q, want %s/%s/%q",
					o.Stage, o.ExcludeReason, o.ExcludeEvidence, tc.stage, tc.reason, tc.evidence)
			}
		})
	}
}

func TestPostLLMExclusionUnit(t *testing.T) {
	yes := true
	no := false
	five, three := 5, 3
	quote := "no sponsorship here"

	tests := []struct {
		name        string
		a           *Analysis
		maxYears    int
		wantReason  string
		wantOrEmpty bool // true: expect no exclusion
	}{
		{"sponsorship no with quote", &Analysis{Sponsorship: "no", SponsorshipQuote: &quote}, 3, "llm:sponsorship_no", false},
		{"sponsorship no without quote", &Analysis{Sponsorship: "no"}, 3, "", true},
		{"years over", &Analysis{YearsRequired: &five}, 3, "llm:years>3", false},
		{"years at max", &Analysis{YearsRequired: &three}, 3, "", true},
		{"non us", &Analysis{USEligible: &no}, 3, "llm:non_us", false},
		{"us eligible", &Analysis{USEligible: &yes}, 3, "", true},
		{"senior", &Analysis{Seniority: "senior"}, 3, "llm:senior", false},
		{"clean junior", &Analysis{Seniority: "junior", USEligible: &yes}, 3, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, _ := PostLLMExclusion(tc.a, tc.maxYears)
			if tc.wantOrEmpty && reason != "" {
				t.Errorf("want no exclusion, got %q", reason)
			}
			if !tc.wantOrEmpty && reason != tc.wantReason {
				t.Errorf("reason: got %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

func TestScorerBudgetGuard(t *testing.T) {
	// One call costs (100*0.15 + 50*0.50)/1e6 = 0.00004. A budget equal to
	// one call stops the run after the first job; with Concurrency=1 the
	// remaining jobs are skipped and never sent.
	s, script := newTestScorer(t, validReply, validReply, validReply)
	s.MaxCostUSD = 0.00004
	outs := s.Score(context.Background(), testJobs(3))

	scoredOrExcluded, skipped := 0, 0
	for _, o := range outs {
		switch o.Stage {
		case StageScored, StageExcluded:
			scoredOrExcluded++
		case StageSkipped:
			skipped++
		}
	}
	if scoredOrExcluded != 1 || skipped != 2 {
		t.Errorf("budget guard: scored/excluded=%d skipped=%d, want 1/2", scoredOrExcluded, skipped)
	}
	if n := script.requestCount(); n != 1 {
		t.Errorf("want 1 request under budget, got %d", n)
	}
}

func TestScorerSkipsOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream broke", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m"})
	s := &Scorer{
		Client:      client,
		Profile:     []byte(testProfile),
		ProfileHash: ProfileHash([]byte(testProfile)),
		Model:       "m",
		Concurrency: 2,
	}
	outs := s.Score(context.Background(), testJobs(2))
	for _, o := range outs {
		if o.Stage != StageSkipped || o.Err == nil {
			t.Errorf("want skipped-with-error, got stage=%q err=%v", o.Stage, o.Err)
		}
	}
}

package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/store"
	"wera/internal/testutil"
)

// fakeCoral answers facts and fit calls like the inference API would and
// counts each kind. A posting mentioning "8+ years" gets 8 required years
// in its facts; every fit scores 81.
type fakeCoral struct {
	facts, fits atomic.Int64
}

func (f *fakeCoral) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	system, last := req.Messages[0].Content, req.Messages[len(req.Messages)-1].Content
	var content string
	switch {
	case strings.HasPrefix(system, "You extract facts"):
		f.facts.Add(1)
		years := 1
		if strings.Contains(last, "8+ years") {
			years = 8
		}
		content = `{"seniority":"junior","years_required":` + strconv.Itoa(years) + `,"sponsorship":"unknown",` +
			`"sponsorship_quote":null,"us_eligible":true,"work_mode":"remote","location_summary":"Remote, US",` +
			`"skills_required":["Go","Kubernetes"],"digest":"Runs production infrastructure."}`
	case strings.HasPrefix(system, "You score how well"):
		f.fits.Add(1)
		content = `{"fit_score":81,"verdict":"good","skills_matched":["Go"],"skills_missing":[],"reason":"Matches the target role."}`
	default:
		http.Error(w, "unexpected prompt", http.StatusBadRequest)
		return
	}
	reply, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
		"usage": map[string]any{"prompt_tokens": 900, "completion_tokens": 80,
			"prompt_tokens_details": map[string]int{"cached_tokens": 600}},
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(reply)
}

func testEngine(t *testing.T) *filter.Engine {
	t.Helper()
	roles, err := config.LoadRoles("../../config/roles.yaml")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := filter.New(roles)
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func stageOf(t *testing.T, pool *pgxpool.Pool, userID, jobID int64) (stage, reason string) {
	t.Helper()
	var r *string
	if err := pool.QueryRow(context.Background(),
		`SELECT stage, exclude_reason FROM user_jobs WHERE user_id = $1 AND job_id = $2`, userID, jobID).Scan(&stage, &r); err != nil {
		t.Fatalf("user_jobs row for job %d: %v", jobID, err)
	}
	if r != nil {
		reason = *r
	}
	return stage, reason
}

func testEnv(url string) *config.Env {
	return &config.Env{
		CoralAPIKey: "cb_test", CoralBaseURL: url,
		CoralModel: "deepseek-v4.1-flash-fast", ScoringConcurrency: 2,
	}
}

var srePrefs = config.Preferences{
	RoleFamilies: []string{"sre", "platform"}, Levels: []string{"entry", "mid"},
	MaxYearsRequired: 3, USOnly: true, NeedsSponsorship: true,
}

const sreProfile = "# Candidate Profile\n## Target roles\nSite reliability and platform engineering, entry level.\n## Technical skills\nGo, Kubernetes, Terraform, Prometheus."

// TestMatchingEndToEnd runs the rule filter, local ranking, shared facts,
// fact-based exclusions and fit scoring for two users with a fake LLM, and
// checks that the second user reuses the first user's facts.
func TestMatchingEndToEnd(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()
	coral := &fakeCoral{}
	srv := httptest.NewServer(coral)
	t.Cleanup(srv.Close)
	env := testEnv(srv.URL)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := testEngine(t)

	companyID, _ := testutil.Company(t, pool, "ai_ml")
	ids := testutil.Jobs(t, pool, companyID,
		testutil.Job{Title: "Site Reliability Engineer", Location: "Remote, US", Description: "Keep our Go services healthy with Kubernetes."},
		testutil.Job{Title: "Senior Site Reliability Engineer", Location: "Remote, US", Description: "Lead reliability."},
		testutil.Job{Title: "Site Reliability Engineer", Location: "London, United Kingdom", Description: "Join our London team."},
		testutil.Job{Title: "Platform Engineer", Location: "Austin, TX", Description: "We are unable to sponsor visas for this position."},
		testutil.Job{Title: "Platform Engineer, Infrastructure", Location: "Seattle, WA", Description: "You have 8+ years of experience running Kubernetes."},
		testutil.Job{Title: "Accountant", Location: "Remote, US", Description: "Close the books."},
	)
	good, senior, uk, noSponsor, tooSenior, offTopic := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]

	u1 := testutil.User(t, pool, false)
	p1 := testutil.Profile(t, pool, u1.ID, sreProfile, srePrefs)
	if _, err := FilterUser(ctx, pool, eng, p1, nil); err != nil {
		t.Fatalf("filter: %v", err)
	}
	for id, want := range map[int64]string{senior: "title:", uk: "location:non_us", noSponsor: "sponsorship:explicit_no", offTopic: "title:no_category"} {
		if stage, reason := stageOf(t, pool, u1.ID, id); stage != "excluded" || !strings.HasPrefix(reason, want) {
			t.Errorf("job %d: got %s/%s, want excluded/%s", id, stage, reason, want)
		}
	}
	for _, id := range []int64{good, tooSenior} {
		if stage, _ := stageOf(t, pool, u1.ID, id); stage != "pending_score" {
			t.Errorf("job %d: got %s, want pending_score", id, stage)
		}
	}
	var est *int
	pool.QueryRow(ctx, `SELECT estimated_score FROM user_jobs WHERE user_id = $1 AND job_id = $2`, u1.ID, good).Scan(&est)
	if est == nil {
		t.Error("pending match has no local estimate")
	}

	st, err := ScoreUser(ctx, pool, env, log, p1, 0, []int64{good, tooSenior}, 1, nil)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if st.FactsExtracted != 2 || st.ExcludedByFacts != 1 || st.Scored != 1 {
		t.Errorf("stats: %+v", st)
	}
	if stage, reason := stageOf(t, pool, u1.ID, tooSenior); stage != "excluded" || !strings.Contains(reason, "years") {
		t.Errorf("8+ years job: %s/%s", stage, reason)
	}
	if stage, _ := stageOf(t, pool, u1.ID, good); stage != "scored" {
		t.Errorf("good job: %s", stage)
	}
	jobs, _, err := store.TodayJobs(ctx, pool, u1.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range jobs {
		if j.ID == good && j.FitScore != nil && *j.FitScore == 81 {
			found = true
		}
	}
	if !found {
		t.Error("scored job missing from Today with its score")
	}
	if coral.facts.Load() != 2 || coral.fits.Load() != 1 {
		t.Errorf("LLM calls: %d facts, %d fits", coral.facts.Load(), coral.fits.Load())
	}

	// A second user with the same preferences: facts are shared, so the
	// too-senior job drops out at filter time and only one fit call is made.
	u2 := testutil.User(t, pool, false)
	p2 := testutil.Profile(t, pool, u2.ID, sreProfile+"\nPrefers remote work.", srePrefs)
	if _, err := FilterUser(ctx, pool, eng, p2, nil); err != nil {
		t.Fatalf("filter user 2: %v", err)
	}
	if stage, _ := stageOf(t, pool, u2.ID, tooSenior); stage != "excluded" {
		t.Errorf("known facts not applied at filter time: %s", stage)
	}
	if _, err := ScoreUser(ctx, pool, env, log, p2, 0, []int64{good}, 1, nil); err != nil {
		t.Fatalf("score user 2: %v", err)
	}
	if coral.facts.Load() != 2 || coral.fits.Load() != 2 {
		t.Errorf("user 2 should reuse facts: %d facts, %d fits", coral.facts.Load(), coral.fits.Load())
	}
}

func TestScoreUserWithoutKeySkips(t *testing.T) {
	st, err := ScoreUser(context.Background(), nil, &config.Env{}, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Profile{}, 0, nil, 1, nil)
	if st != nil || err != nil {
		t.Errorf("want (nil, nil) without an API key, got %v, %v", st, err)
	}
}

func TestExclusionsFor(t *testing.T) {
	r := ExclusionsFor(&config.Preferences{MaxYearsRequired: 3, NeedsSponsorship: true, USOnly: true, Levels: []string{"entry"}})
	if r.MaxYears != 3 || !r.RequireSponsorship || !r.USOnly || r.AllowSenior {
		t.Errorf("%+v", r)
	}
	if !ExclusionsFor(&config.Preferences{Levels: []string{"senior"}}).AllowSenior {
		t.Error("senior level should allow senior roles")
	}
}

func TestScoreStatsAdd(t *testing.T) {
	a := &ScoreStats{Scored: 1, CostUSD: 0.5}
	a.Add(&ScoreStats{Scored: 2, Failed: 1, CostUSD: 0.25})
	a.Add(nil)
	if a.Scored != 3 || a.Failed != 1 || a.CostUSD != 0.75 {
		t.Errorf("%+v", a)
	}
}

package pipeline

import (
	"math"
	"testing"

	"wera/internal/config"
	"wera/internal/scoring"
)

func TestBlendRanksFitAndPenalizesYears(t *testing.T) {
	md := "# Profile\n## Target roles\n- Registered Nurse, ICU\n## Skills\n- ICU, telemetry, BLS, ACLS, Epic\n## Experience\n- Hospital work"
	years := func(n int) *int { return &n }
	facts := map[int64]*scoring.Facts{
		1: {Seniority: "mid", SkillsRequired: []string{"ICU", "BLS", "ACLS"}, Digest: "Registered nurse for a 24-bed ICU.", YearsRequired: years(2)},
		2: {Seniority: "mid", SkillsRequired: []string{"Spark", "Airflow"}, Digest: "Data engineer building pipelines.", YearsRequired: years(2)},
		3: {Seniority: "senior", SkillsRequired: []string{"ICU", "BLS", "ACLS"}, Digest: "Registered nurse for a 24-bed ICU.", YearsRequired: years(8)},
	}
	titles := map[int64]string{1: "ICU Registered Nurse", 2: "Data Engineer", 3: "ICU Registered Nurse"}
	prefs := config.Preferences{MaxYearsRequired: 5}

	got := Blend(md, prefs, titles, facts, map[int64]float64{1: 0.8, 2: 0.3, 3: 0.8})
	if !(got[1] > got[2]) {
		t.Errorf("the matching job should rank above the unrelated one: %v", got)
	}
	if !(got[1] > got[3]+3) {
		t.Errorf("the same job asking more years than the user accepts should rank well below: %v", got)
	}
	// Without embeddings, words and skills still rank.
	if noEmb := Blend(md, prefs, titles, facts, nil); !(noEmb[1] > noEmb[2]) {
		t.Errorf("blend without embeddings: %v", noEmb)
	}
	if Blend(md, prefs, titles, nil, nil) != nil {
		t.Error("no facts, nothing to blend")
	}
}

// pair blends two jobs among a few others (signals are standardized, so two
// jobs alone would make any difference look large) and returns the first
// job's lead over the second.
func pair(md string, prefs config.Preferences, a, b scoring.Facts) float64 {
	titles := map[int64]string{1: "Engineer", 2: "Engineer"}
	facts := map[int64]*scoring.Facts{1: &a, 2: &b}
	for i, d := range []string{"Run Go services.", "Tune Postgres replication.", "Ship Docker images.", "Sell enterprise software.", "Care for patients on a ward.", "Teach high school math."} {
		id := int64(i + 3)
		titles[id] = "Engineer"
		facts[id] = &scoring.Facts{Seniority: "mid", SkillsRequired: []string{"Go", "Excel", "Teaching"}, Digest: d, WorkMode: "hybrid"}
	}
	got := Blend(md, prefs, titles, facts, nil)
	return got[1] - got[2]
}

func TestBlendFollowsWhatTheFitScoreWeighs(t *testing.T) {
	md := "# Profile\n## Target roles\n- Engineer\n## Skills\n- Go, Postgres, Docker\n## Experience\n- Built Kubernetes platforms"
	years := func(n int) *int { return &n }
	job := scoring.Facts{Seniority: "mid", SkillsRequired: []string{"Go", "Postgres", "Docker"}, Digest: "Build services."}
	with := func(f func(*scoring.Facts)) scoring.Facts {
		j := job
		f(&j)
		return j
	}
	prefs := config.Preferences{Levels: []string{"entry", "mid"}, MaxYearsRequired: 5}

	// Kubernetes is only in the experience section: the fit score sees it,
	// and so must the ranking.
	covered := with(func(j *scoring.Facts) { j.SkillsRequired = []string{"Kubernetes", "Go", "Postgres"} })
	missing := with(func(j *scoring.Facts) { j.SkillsRequired = []string{"Terraform", "Go", "Postgres"} })
	if d := pair(md, prefs, covered, missing); d <= 0 {
		t.Errorf("a skill anywhere in the profile should count: %v", d)
	}
	if d := pair(md, prefs, with(func(j *scoring.Facts) { j.YearsRequired = years(1) }), with(func(j *scoring.Facts) { j.YearsRequired = years(5) })); d <= 0 {
		t.Errorf("asking near the limit should rank lower: %v", d)
	}
	if d := pair(md, prefs, job, with(func(j *scoring.Facts) { j.Seniority = "senior" })); d <= 0 {
		t.Errorf("a level the user does not accept should rank lower: %v", d)
	}
	if d := pair(md, prefs, with(func(j *scoring.Facts) { j.WorkMode = "remote" }), with(func(j *scoring.Facts) { j.WorkMode = "onsite" })); d <= 0 {
		t.Errorf("remote should rank higher: %v", d)
	}
}

func TestBlocked(t *testing.T) {
	for digest, want := range map[string]bool{
		"Requires an active secret clearance.": true,
		"Must hold TS/SCI.":                    true,
		"DoD clearance needed.":                true,
		"US citizenship required.":             true,
		"U.S. citizen only.":                   true,
		"Must be clearable.":                   true,
		"ITAR controlled work.":                true,
		"Medical clearance on hire.":           false,
		"Work at a clearance outlet.":          false,
		"Build for us, citizens of the web.":   false,
		"Reduce the clearing time of trades.":  false,
	} {
		if got := blocked(&scoring.Facts{Digest: digest}); got != want {
			t.Errorf("blocked(%q) = %v, want %v", digest, got, want)
		}
	}
	if !blocked(&scoring.Facts{Digest: "Build.", SkillsRequired: []string{"Go", "Top Secret clearance"}}) {
		t.Error("a demand may be listed among the skills")
	}
}

func TestBlendPenalizesBlockersOnlyForThoseTheyBlock(t *testing.T) {
	md := "# Profile\n## Target roles\n- Engineer\n## Skills\n- Go\n## Experience\n- Built services"
	cleared := md + "\n- Holds an active secret clearance"
	plain := scoring.Facts{Seniority: "mid", SkillsRequired: []string{"Go"}, Digest: "Build services."}
	demand := plain
	demand.Digest = "Build services. Requires an active secret clearance."
	prefs := config.Preferences{Levels: []string{"mid"}}
	sponsored := prefs
	sponsored.NeedsSponsorship = true

	// The same two jobs and the same profile text: only the penalty differs.
	if got := pair(cleared, sponsored, plain, demand) - pair(cleared, prefs, plain, demand); math.Abs(got-penaltyBlocker) > 1e-6 {
		t.Errorf("a user needing sponsorship cannot clear: extra lead %.3f, want %.1f", got, penaltyBlocker)
	}
	if got := pair(md, prefs, plain, demand) - pair(cleared, prefs, plain, demand); got < penaltyBlocker/2 {
		t.Errorf("a user who shows no clearance should be penalized, one who does not: %.3f", got)
	}
}

func TestLevelGap(t *testing.T) {
	p := config.Preferences{Levels: []string{"entry", "mid"}}
	for level, want := range map[string]float64{"entry": 0, "mid": 0, "junior": 1, "senior": 1, "unknown": 1, "": 1} {
		if got := levelGap(p, level); got != want {
			t.Errorf("levelGap(%q) = %v, want %v", level, got, want)
		}
	}
	if got := levelGap(config.Preferences{Levels: []string{"management"}}, "entry"); got != 4 {
		t.Errorf("far apart: %v", got)
	}
	if got := levelGap(config.Preferences{}, "senior"); got != 1 {
		t.Errorf("no levels set: %v", got)
	}
}

func TestEmbedderKeysMarkPrefixes(t *testing.T) {
	e := &Embedder{Model: "BAAI/bge-small-en-v1.5"}
	if e.profileKey() != e.Model || e.jobKey() != e.Model {
		t.Errorf("no prefix keeps the model as the key: %q %q", e.profileKey(), e.jobKey())
	}
	e.QueryPrefix = "Represent this sentence for searching relevant passages: "
	if e.profileKey() == e.Model || e.jobKey() != e.Model {
		t.Errorf("a query prefix must change only the profile key: %q %q", e.profileKey(), e.jobKey())
	}
	env := &config.Env{EmbedBaseURL: "http://embed/v1", EmbedModel: "intfloat/e5-base-v2"}
	if ne := NewEmbedder(env, nil); ne.QueryPrefix != "query: " || ne.DocPrefix != "passage: " {
		t.Errorf("model defaults: %+v", ne)
	}
	none := ""
	env.EmbedQueryPrefix, env.EmbedDocPrefix = &none, &none
	if ne := NewEmbedder(env, nil); ne.QueryPrefix != "" || ne.DocPrefix != "" {
		t.Errorf("set-but-empty prefixes must win: %+v", ne)
	}
}

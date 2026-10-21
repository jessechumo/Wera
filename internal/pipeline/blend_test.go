package pipeline

import (
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

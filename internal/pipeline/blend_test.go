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

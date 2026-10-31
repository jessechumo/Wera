package relevance

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	got := Tokens("Senior C++ / Node.js Engineer: build CI/CD and Machine Learning in R and Go!")
	want := []string{"senior", "cplusplus", "nodejs", "engineer", "cicd", "machinelearning", "r", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens = %v, want %v", got, want)
	}
}

func TestRankOrdersByRelevance(t *testing.T) {
	profile := "Data scientist. Python, SQL, PyTorch, scikit-learn, gradient boosting. Flight delay prediction for an airline."
	docs := []Doc{
		{ID: 1, Title: "Account Executive", Body: "Sell enterprise software to large customers. Quota, pipeline, CRM."},
		{ID: 2, Title: "Data Scientist, Flight Operations", Body: "Build delay prediction models in Python and SQL with gradient boosting."},
		{ID: 3, Title: "Backend Engineer", Body: "Go and PostgreSQL services; some Python scripting."},
	}
	got := Rank(profile, docs)
	if got[2] <= got[3] || got[3] <= got[1] {
		t.Fatalf("want data scientist > backend > sales, got %v", got)
	}
	if got[2] != 85 || got[1] != 35 {
		t.Errorf("scores should span 35..85, got %v", got)
	}
	if len(Rank(profile, nil)) != 0 {
		t.Error("no docs, no scores")
	}
}

func TestRankKeepsTopMatchesApart(t *testing.T) {
	profile := "go kubernetes terraform prometheus linux networking"
	var docs []Doc
	// 200 filler postings, then five whose overlap with the profile grows.
	for i := range 200 {
		docs = append(docs, Doc{ID: int64(1000 + i), Title: "Sales", Body: "quota pipeline crm territory"})
	}
	words := []string{"go", "kubernetes", "terraform", "prometheus", "linux"}
	for k := 1; k <= 5; k++ {
		docs = append(docs, Doc{ID: int64(k), Title: "Engineer", Body: strings.Join(words[:k], " ") + " docs testing"})
	}
	got := Rank(profile, docs)
	for k := 1; k < 5; k++ {
		if got[int64(k)] >= got[int64(k+1)] {
			t.Fatalf("more overlap must score higher: %d -> %d, %d -> %d", k, got[int64(k)], k+1, got[int64(k+1)])
		}
	}
}

func TestSimilaritiesAndSkillOverlap(t *testing.T) {
	docs := []Doc{
		{ID: 1, Title: "Registered Nurse", Body: "ICU telemetry, BLS, ACLS"},
		{ID: 2, Title: "Data Engineer", Body: "Spark, Airflow, SQL"},
	}
	sims := Similarities("Registered nurse with ICU and telemetry experience; BLS and ACLS.", docs)
	if sims[1] <= sims[2] {
		t.Errorf("the nursing job should be closer: %v", sims)
	}
	if Similarities("the and of", docs) != nil {
		t.Error("a profile with no usable words should give nil")
	}
	have := TokenSet("Skills: Python, SQL, Apache Spark")
	if got := SkillOverlap(have, []string{"Python", "Spark", "Kubernetes", "SQL"}); got != 0.75 {
		t.Errorf("overlap = %v, want 0.75", got)
	}
	if got := SkillOverlap(have, nil); got != 0.5 {
		t.Errorf("no listed skills should be neutral, got %v", got)
	}
}

func TestStandardize(t *testing.T) {
	z := Standardize(map[int64]float64{1: 1, 2: 2, 3: 3})
	if z[2] != 0 || z[1] >= 0 || z[3] <= 0 || math.Abs(z[1]+z[3]) > 1e-9 {
		t.Errorf("standardize = %v", z)
	}
	for _, v := range Standardize(map[int64]float64{1: 5, 2: 5}) {
		if v != 0 {
			t.Errorf("equal values should all be 0, got %v", v)
		}
	}
}

func TestScaleExactKeepsOrderWithinAWholeEstimate(t *testing.T) {
	sims := map[int64]float64{1: 0, 2: 1, 3: 0.5001, 4: 0.5002}
	exact, whole := ScaleExact(sims), Scale(sims)
	if exact[1] != 35 || exact[2] != 85 || whole[1] != 35 || whole[2] != 85 {
		t.Errorf("ends: %v %v", exact, whole)
	}
	if whole[3] != whole[4] || !(exact[4] > exact[3]) {
		t.Errorf("close jobs tie when whole and keep their order when exact: %v %v", whole, exact)
	}
	if got := ScaleExact(map[int64]float64{7: 3}); got[7] != 85 {
		t.Errorf("a single job: %v", got)
	}
}

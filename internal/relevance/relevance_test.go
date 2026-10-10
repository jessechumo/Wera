package relevance

import (
	"reflect"
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
	if !(got[2] > got[3] && got[3] > got[1]) {
		t.Fatalf("want data scientist > backend > sales, got %v", got)
	}
	if got[2] != 85 || got[1] != 35 {
		t.Errorf("scores should span 35..85, got %v", got)
	}
	if len(Rank(profile, nil)) != 0 {
		t.Error("no docs, no scores")
	}
}

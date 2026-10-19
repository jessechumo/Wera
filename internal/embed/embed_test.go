package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedBatchesAndNormalizes(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		calls++
		var in struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Model != "m" {
			http.Error(w, "model", http.StatusBadRequest)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		var data []item
		// Answer out of order to check that index is honored.
		for i := len(in.Input) - 1; i >= 0; i-- {
			data = append(data, item{Index: i, Embedding: []float32{float32(len(in.Input[i])), 0, 0}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1/", "m", "k")
	texts := make([]string, 70)
	for i := range texts {
		texts[i] = strings.Repeat("x", i+1)
	}
	vecs, err := c.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 70 || calls != 3 {
		t.Fatalf("got %d vectors in %d calls, want 70 in 3", len(vecs), calls)
	}
	for i, v := range vecs {
		if math.Abs(float64(v[0])-1) > 1e-6 {
			t.Fatalf("vector %d not normalized: %v", i, v)
		}
	}
}

func TestEmbedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model loading", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "m", "").Embed(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("want a 503 error, got %v", err)
	}
	if New("", "m", "") != nil {
		t.Fatal("no endpoint should mean no client")
	}
}

func TestCosine(t *testing.T) {
	a := Normalize([]float32{1, 1})
	b := Normalize([]float32{1, 0})
	if got := Cosine(a, b); math.Abs(got-math.Sqrt2/2) > 1e-6 {
		t.Fatalf("cosine = %v", got)
	}
	if Cosine(a, []float32{1}) != 0 {
		t.Fatal("different sizes should give 0")
	}
}

func TestProfileText(t *testing.T) {
	md := "# Candidate Profile\n\n## Target roles\n- ML Engineer\n\n## Education\n- Princeton\n\n## Technical skills\n- Python, CUDA\n"
	got := ProfileText(md)
	if !strings.Contains(got, "ML Engineer") || !strings.Contains(got, "CUDA") || strings.Contains(got, "Princeton") {
		t.Fatalf("ProfileText kept the wrong sections:\n%s", got)
	}
	if ProfileText("just some text") != "just some text" {
		t.Fatal("a profile without sections should be used whole")
	}
}

func TestJobText(t *testing.T) {
	got := JobText("Backend Engineer", "mid", []string{"Go", "Postgres"}, "Builds APIs.")
	if got != "Backend Engineer. Seniority: mid. Skills: Go, Postgres. Builds APIs." {
		t.Fatalf("JobText = %q", got)
	}
}

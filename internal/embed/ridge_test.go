package embed

import (
	"math"
	"math/rand/v2"
	"testing"
)

// unit returns a random unit vector.
func unit(r *rand.Rand, d int) []float32 {
	v := make([]float32, d)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	return Normalize(v)
}

func TestRidgeRecoversALinearRule(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	const d = 16
	truth := unit(r, d)
	score := func(v []float32) float64 { return 50 + 40*Cosine(truth, v) }
	x, y := make([][]float32, 0, 400), make([]float64, 0, 400)
	for range 400 {
		v := unit(r, d)
		x, y = append(x, v), append(y, score(v))
	}
	m := FitRidge(x, y, 0.01)
	if m == nil {
		t.Fatal("no model")
	}
	for range 50 {
		v := unit(r, d)
		if got, want := m.Predict(v), score(v); math.Abs(got-want) > 0.5 {
			t.Fatalf("predict = %.2f, want %.2f", got, want)
		}
	}
}

// The usual case for a new user: far fewer scores than dimensions.
func TestRidgeWithFewerExamplesThanDimensions(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	const d = 64
	good, bad := unit(r, d), unit(r, d)
	x, y := make([][]float32, 0, 20), make([]float64, 0, 20)
	for i := range 20 {
		v, s := good, 90.0
		if i%2 == 1 {
			v, s = bad, 20.0
		}
		x, y = append(x, v), append(y, s)
	}
	m := FitRidge(x, y, 1)
	if m == nil {
		t.Fatal("no model")
	}
	if g, b := m.Predict(good), m.Predict(bad); g <= b+40 {
		t.Errorf("good %.1f should be far above bad %.1f", g, b)
	}
}

func TestRidgeShrinksWithLambda(t *testing.T) {
	x := [][]float32{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	y := []float64{10, 0, -10, 0}
	loose, tight := FitRidge(x, y, 0.001), FitRidge(x, y, 100)
	if a, b := loose.Predict([]float32{1, 0}), tight.Predict([]float32{1, 0}); a <= b || b <= 0 {
		t.Errorf("lambda should shrink toward the mean: loose %.3f, tight %.3f", a, b)
	}
}

func TestRidgeCases(t *testing.T) {
	if FitRidge(nil, nil, 1) != nil {
		t.Error("no examples, no model")
	}
	if FitRidge([][]float32{{1}}, []float64{1, 2}, 1) != nil {
		t.Error("mismatched inputs, no model")
	}
	if FitRidge([][]float32{{1}}, []float64{1}, 0) != nil {
		t.Error("lambda must be positive")
	}
	// All scores equal: nothing to learn, every estimate is that score.
	same := FitRidge([][]float32{{1, 0}, {0, 1}, {1, 1}}, []float64{70, 70, 70}, 1)
	if got := same.Predict([]float32{0.5, -3}); math.Abs(got-70) > 1e-9 {
		t.Errorf("predict = %v", got)
	}
	// A vector from another model is left out of the fit, and gets the mean.
	mixed := FitRidge([][]float32{{1, 0}, {0, 1, 0}, {0, 1}}, []float64{80, 999, 40}, 1)
	if got := mixed.Predict([]float32{9, 9, 9}); math.Abs(got-60) > 1e-9 {
		t.Errorf("predict for another length = %v, want the mean 60", got)
	}
}

func BenchmarkFitRidge(b *testing.B) {
	r := rand.New(rand.NewPCG(5, 6))
	x := make([][]float32, 2000)
	y := make([]float64, len(x))
	for i := range x {
		x[i], y[i] = unit(r, 768), r.Float64()*100
	}
	for b.Loop() {
		FitRidge(x, y, 1)
	}
}

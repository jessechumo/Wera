package scoring

import "testing"

func TestCostMath(t *testing.T) {
	p, ok := PriceFor("glm-5.3-flash-fast")
	if !ok {
		t.Fatal("missing price for glm-5.3-flash-fast")
	}
	// 10,000 prompt tokens, 6,000 of them cached, 500 completion:
	// (4,000 x 0.15 + 500 x 0.50) / 1e6 = 0.00085
	got := p.CostUSD(10_000, 6_000, 500)
	want := (4_000*0.15 + 500*0.50) / 1e6
	if diff := got - want; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost: got %.9f, want %.9f", got, want)
	}
	if got < 0.000849 || got > 0.000851 {
		t.Errorf("cost sanity: got %.9f, want ~0.00085", got)
	}

	// Fully cached prompt: only completion is charged.
	got = p.CostUSD(10_000, 10_000, 200)
	want = 200 * 0.50 / 1e6
	if got != want {
		t.Errorf("all-cached cost: got %.9f, want %.9f", got, want)
	}

	// Corrupt accounting must never go negative.
	if c := p.CostUSD(100, 500, 10); c < 0 {
		t.Errorf("negative cost: %f", c)
	}
}

func TestCostOtherModels(t *testing.T) {
	tests := []struct {
		model string
		input float64
	}{
		{"glm-5.3-fast", 1.12},
		{"deepseek-v4.1-flash-fast", 0.30},
	}
	for _, tc := range tests {
		p, ok := PriceFor(tc.model)
		if !ok {
			t.Fatalf("missing price for %s", tc.model)
		}
		if p.Input != tc.input {
			t.Errorf("%s input price: got %v, want %v", tc.model, p.Input, tc.input)
		}
	}
}

func TestPriceUnknownModel(t *testing.T) {
	p, ok := PriceFor("no-such-model")
	if ok {
		t.Error("unknown model should not resolve")
	}
	if c := p.CostUSD(1000, 0, 100); c != 0 {
		t.Errorf("unknown model cost should be 0, got %f", c)
	}
}

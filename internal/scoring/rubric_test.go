package scoring

import "testing"

func TestRatingsScore(t *testing.T) {
	tests := []struct {
		name     string
		r        Ratings
		coverage float64
		want     int
	}{
		{"exact target role", Ratings{Role: 10, Seniority: 10, Skills: 10, Domain: 10, Location: 10}, -1, 100},
		{"strong fit", Ratings{Role: 9, Seniority: 9, Skills: 8, Domain: 5, Location: 8}, -1, 83},
		{"coverage lifts the skills rating", Ratings{Role: 9, Seniority: 9, Skills: 8, Domain: 5, Location: 8}, 1, 85},
		{"coverage lowers it", Ratings{Role: 9, Seniority: 9, Skills: 8, Domain: 5, Location: 8}, 0.25, 77},
		// Far from the role or level: scaled down smoothly, so a one-point change never jumps.
		{"unrelated role", Ratings{Role: 2, Seniority: 8, Skills: 3, Domain: 4, Location: 9}, -1, 33},
		{"one point more", Ratings{Role: 3, Seniority: 8, Skills: 3, Domain: 4, Location: 9}, -1, 39},
		{"an intern role for a mid-level candidate", Ratings{Role: 8, Seniority: 0, Skills: 7, Domain: 5, Location: 5}, -1, 39},
		{"a clearance they lack", Ratings{Role: 9, Seniority: 9, Skills: 8, Domain: 5, Location: 8, Blocker: true}, -1, 33},
		{"nothing fits", Ratings{}, -1, 0},
	}
	for _, tc := range tests {
		if got := tc.r.Score(tc.coverage); got != tc.want {
			t.Errorf("%s: Score = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The old single-number score put every strong job at 88: ratings that
// differ a little must give different scores.
func TestRatingsSpread(t *testing.T) {
	seen := map[int]bool{}
	for role := 6; role <= 10; role++ {
		for skills := 5; skills <= 10; skills++ {
			seen[Ratings{Role: role, Seniority: 8, Skills: skills, Domain: 6, Location: 7}.Score(-1)] = true
		}
	}
	if len(seen) < 20 {
		t.Errorf("30 rating combinations gave only %d scores", len(seen))
	}
}

func TestVerdictFor(t *testing.T) {
	for score, want := range map[int]string{100: "strong", 80: "strong", 79: "good", 65: "good", 64: "stretch", 45: "stretch", 44: "poor", 0: "poor"} {
		if got := VerdictFor(score); got != want {
			t.Errorf("VerdictFor(%d) = %q, want %q", score, got, want)
		}
	}
}

func TestCoverage(t *testing.T) {
	if c := Coverage([]string{"Go"}, []string{"Rust"}); c != -1 {
		t.Errorf("two skills: coverage %v, want unknown (-1)", c)
	}
	if c := Coverage([]string{"Go", "SQL", "AWS"}, []string{"Rust"}); c != 0.75 {
		t.Errorf("coverage %v, want 0.75", c)
	}
}

func TestScoreKeyChangesWithVersion(t *testing.T) {
	h := ProfileHash([]byte(testProfile))
	if k := ScoreKey(h); k == h || len(k) != 64 || k != ScoreKey(h) {
		t.Errorf("ScoreKey(%s) = %s: must be a stable 64-char key distinct from the profile hash", h, k)
	}
}

func TestRatingsValid(t *testing.T) {
	if !(Ratings{Role: 10, Seniority: 0, Skills: 5, Domain: 5, Location: 5}).Valid() {
		t.Error("in-range ratings rejected")
	}
	if (Ratings{Role: 11}).Valid() || (Ratings{Location: -1}).Valid() {
		t.Error("out-of-range ratings accepted")
	}
}

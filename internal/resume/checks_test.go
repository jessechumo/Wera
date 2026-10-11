package resume

import "testing"

func TestReview(t *testing.T) {
	good := sample(t)
	good.Sections[2].Entries[0].Bullets[0].Text = "Filed a US Patent on deploys, i.e. faster rollbacks by 40%"
	rv := good.Review(1)
	for _, c := range rv.Checks {
		if c.ID == "pronoun" {
			t.Errorf("false pronoun: %+v", c)
		}
	}
	for _, c := range rv.Checks {
		if c.Severity == "fix" {
			t.Errorf("a solid resume got a must-fix: %+v", c)
		}
	}

	bad := sample(t)
	bad.Email = ""
	exp := &bad.Sections[2].Entries[0]
	exp.Bullets = []Bullet{
		{ID: "b1", Text: "Responsible for the deploy pipeline"},
		{ID: "b2", Text: "I built a dashboard that my team used every day for monitoring the production fleet"},
		{ID: "b3", Text: "Passionate team player"},
	}
	rv2 := bad.Review(2)
	want := map[string]string{"no-email": "", "too-long": "", "weak-opener": "b1", "pronoun": "b2", "cliche": "b3"}
	found := map[string]bool{}
	for _, c := range rv2.Checks {
		if target, ok := want[c.ID]; ok && (target == "" || c.Target == target) {
			found[c.ID] = true
		}
	}
	for id := range want {
		if !found[id] {
			t.Errorf("missing %s in %+v", id, rv2.Checks)
		}
	}
	if rv2.Score >= rv.Score {
		t.Errorf("scores: bad %d should be below good %d", rv2.Score, rv.Score)
	}
}

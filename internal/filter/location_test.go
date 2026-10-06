package filter

import "testing"

func TestApplyLocationRules(t *testing.T) {
	e := newEngine(t)
	const title = "Site Reliability Engineer"
	const desc = "We build reliable systems."
	tests := []struct {
		location string
		excluded bool
	}{
		{"Chicago, IL", false},
		{"New York, NY", false},
		{"Austin, TX (Hybrid)", false},
		{"United States", false},
		{"Remote - US", false},
		{"US Remote", false},
		{"Remote", false},
		{"", false},
		{"Hybrid - Seattle", false},
		{"London, UK", true},
		{"Bangalore, India", true},
		{"Singapore", true},
		{"Toronto, ON, Canada", true},
		{"Hyderabad, Telangana, India", true},
		{"Paris, France", true},
		{"Dublin, Ireland", true},
		{"Tel Aviv, Israel", true},
	}
	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			res := e.Apply(title, tc.location, desc)
			if res.Stage == StageExcluded != tc.excluded {
				t.Fatalf("location %q: excluded=%v, want %v (reason=%q)",
					tc.location, res.Stage == StageExcluded, tc.excluded, res.Reason)
			}
			if tc.excluded && res.Reason != "location:non_us" {
				t.Errorf("reason: got %q, want location:non_us", res.Reason)
			}
		})
	}
}

func TestApplyFlags(t *testing.T) {
	e := newEngine(t)
	// A hard sponsorship exclude in the same description still drops the job.
	res := e.Apply("Site Reliability Engineer", "Chicago, IL",
		"This position requires access to ITAR-controlled technical data. Only U.S. persons may apply. We do not sponsor visas anyway.")
	if res.Stage != StageExcluded {
		t.Fatalf("expected exclude, got %s", res.Stage)
	}

	res = e.Apply("Site Reliability Engineer", "Chicago, IL",
		"This position requires access to ITAR-controlled technical data. Only U.S. persons may apply.")
	if res.Stage != StagePendingScore {
		t.Fatalf("expected keep, got %s (%s)", res.Stage, res.Reason)
	}
	if len(res.Flags) == 0 {
		t.Fatal("expected flags from ITAR patterns")
	}
	for _, f := range res.Flags {
		if f != "itar" && f != "u_s_persons" && f != "export_control" {
			t.Errorf("unexpected flag %q", f)
		}
	}
}

func TestEvidenceIsTheMatchedSentence(t *testing.T) {
	e := newEngine(t)
	desc := "We offer visa sponsorship. IBM will not be providing visa sponsorship for this position now or in the future. Please apply today."
	res := e.Apply("Site Reliability Engineer", "Chicago, IL", desc)
	if res.Stage != StageExcluded {
		t.Fatalf("expected exclude, got %s", res.Stage)
	}
	want := "IBM will not be providing visa sponsorship for this position now or in the future."
	if res.Evidence != want {
		t.Errorf("evidence: got %q, want %q", res.Evidence, want)
	}
}

func TestTag(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Senior", "senior"},
		{"Sr.", "sr"},
		{"Staff", "staff"},
		{"Intern", "intern"},
		{"ITAR", "itar"},
		{"U.S. persons", "u_s_persons"},
		{"export control", "export_control"},
	}
	for _, tc := range tests {
		if got := Tag(tc.in); got != tc.want {
			t.Errorf("Tag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

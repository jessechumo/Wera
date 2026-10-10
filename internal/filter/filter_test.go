package filter

import (
	"path/filepath"
	"testing"

	"wera/internal/config"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	r, err := config.LoadRoles(filepath.Join("..", "..", "config", "roles.yaml"))
	if err != nil {
		t.Fatalf("load roles.yaml: %v", err)
	}
	e, err := New(r)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

// infraPrefs reproduces the original single-user rules: entry/junior
// infrastructure roles in the US, sponsorship required.
var infraPrefs = &config.Preferences{
	RoleFamilies: []string{"sre", "platform", "devops", "infrastructure", "swe_infra",
		"production", "trading_ops", "ml_infra", "early_career"},
	Levels:           []string{"entry", "mid"},
	MaxYearsRequired: 3,
	USOnly:           true,
	NeedsSponsorship: true,
}

func TestApplyTitleRules(t *testing.T) {
	e := newEngine(t)
	const neutralDesc = "We build reliable systems and welcome new graduates."
	tests := []struct {
		title    string
		excluded bool
		reason   string
		category string // must be among matched categories (if kept)
	}{
		{"Site Reliability Engineer", false, "", "sre"},
		{"SRE Monitoring Platform Software Engineer (Entry Level)", false, "", "sre"},
		{"Infrastructure Software Engineer", false, "", "infrastructure"},
		{"Software Engineer - Dedicated Inference", false, "", "ml_infra"},
		{"Software Engineer, Infrastructure", false, "", "swe_infra"},
		{"Trade Systems Engineer", false, "", "trading_ops"},
		{"Junior Cloud Platform Engineer", false, "", "platform"},
		{"Member of Technical Staff - Systems", false, "", "mts_infra"},
		{"Senior Site Reliability Engineer", true, "title:senior", ""},
		{"Staff Software Engineer, Infrastructure", true, "title:staff", ""},
		{"Member of Technical Staff - Product Design", true, "title:staff", ""},
		{"Software Engineer Intern, Infrastructure", true, "title:intern", ""},
		{"Software Engineer, Frontend", true, "title:no_category", ""},
		{"Account Executive - AI Native", true, "title:no_category", ""},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			res := e.Apply(infraPrefs, tc.title, "Chicago, IL", neutralDesc)
			if res.Stage == StageExcluded && !tc.excluded {
				t.Fatalf("kept expected, got exclude %q (%q)", res.Reason, res.Evidence)
			}
			if res.Stage != StageExcluded && tc.excluded {
				t.Fatalf("exclude %q expected, got stage %s", tc.reason, res.Stage)
			}
			if tc.excluded && res.Reason != tc.reason {
				t.Fatalf("reason: got %q, want %q", res.Reason, tc.reason)
			}
			if !tc.excluded {
				found := false
				for _, c := range res.Categories {
					if c == tc.category {
						found = true
					}
				}
				if !found {
					t.Fatalf("categories %v missing %q", res.Categories, tc.category)
				}
			}
		})
	}
}

func TestApplySponsorshipRules(t *testing.T) {
	e := newEngine(t)
	const title = "Site Reliability Engineer"
	const location = "Chicago, IL"
	tests := []struct {
		name     string
		desc     string
		excluded bool
	}{
		{"ibm explicit no", "IBM will not be providing visa sponsorship for this position now or in the future.", true},
		{"swing unable to provide", "Swing Education is unable to provide employment visa sponsorship, including H-1B sponsorship, for this position.", true},
		{"authorized without sponsorship", "Applicants must be currently authorized to work in the United States on a full-time basis without current or future sponsorship.", true},
		{"not able to sponsor", "We are not able to sponsor visas for this role.", true},
		{"sponsorship not available", "Sponsorship is not available for this position.", true},
		{"without need for sponsorship", "Candidates must have the ability to work without a need for current or future visa sponsorship.", true},
		{"do not sponsor", "We do not sponsor visas.", true},
		{"cannot sponsor", "The company cannot sponsor employment visas at this time.", true},
		{"no visa sponsorship", "No visa sponsorship is available.", true},
		{"available", "Sponsorship is available for this role.", false},
		{"may be available", "Visa sponsorship (including H-1B) may be available for this position.", false},
		{"we offer", "We offer visa sponsorship.", false},
		{"opt cpt", "Eligible for visa sponsorship and open to candidates with OPT/CPT.", false},
		{"no relocation requirement", "There is no requirement to relocate.", false},
		{"sponsorship without exception", "We provide sponsorship for qualified candidates without exception.", false},
		{"not required to apply", "Sponsorship is not required to apply; we sponsor visas.", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := e.Apply(infraPrefs, title, location, tc.desc)
			if res.Stage == StageExcluded != tc.excluded {
				t.Fatalf("excluded=%v, want %v (reason=%q evidence=%q)",
					res.Stage == StageExcluded, tc.excluded, res.Reason, res.Evidence)
			}
			if tc.excluded {
				if res.Reason != "sponsorship:explicit_no" {
					t.Errorf("reason: got %q, want sponsorship:explicit_no", res.Reason)
				}
				if res.Evidence != tc.desc {
					t.Errorf("evidence: got %q, want the matched sentence %q", res.Evidence, tc.desc)
				}
			}
		})
	}
}

func TestApplyOtherUsers(t *testing.T) {
	e := newEngine(t)
	const desc = "We do not sponsor visas."
	dataSWE := &config.Preferences{
		RoleFamilies: []string{"software_engineering", "data_science", "ml_engineering"},
		Levels:       []string{"entry", "mid", "senior"},
		USOnly:       true,
	}
	anywhere := &config.Preferences{
		RoleFamilies: []string{"data_science"},
		Levels:       []string{"internship", "entry"},
	}
	tests := []struct {
		name     string
		prefs    *config.Preferences
		title    string
		location string
		reason   string // "" = kept
		category string
	}{
		{"swe kept", dataSWE, "Software Engineer, Frontend", "Seattle, WA", "", "software_engineering"},
		{"senior accepted", dataSWE, "Senior Data Scientist, Flight Operations", "Dallas, TX", "", "data_science"},
		{"no sponsorship needed", dataSWE, "Machine Learning Engineer", "Remote - US", "", "ml_engineering"},
		{"mts generic", dataSWE, "Member of Technical Staff", "San Francisco, CA", "", "mts_software"},
		{"management not accepted", dataSWE, "Director of Data Science", "Austin, TX", "title:director", ""},
		{"family not selected", dataSWE, "Site Reliability Engineer", "Austin, TX", "title:no_category", ""},
		{"non-US dropped", dataSWE, "Data Scientist", "London, UK", "location:non_us", ""},
		{"non-US allowed", anywhere, "Data Scientist", "London, UK", "", "data_science"},
		{"internship accepted", anywhere, "Data Science Intern", "Chicago, IL", "", "data_science"},
		{"mid-level marker free", anywhere, "Data Scientist II", "Chicago, IL", "", "data_science"},
		{"senior not accepted", anywhere, "Staff Data Scientist", "Chicago, IL", "title:staff", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := e.Apply(tc.prefs, tc.title, tc.location, desc)
			if tc.reason != "" {
				if res.Stage != StageExcluded || res.Reason != tc.reason {
					t.Fatalf("want excluded %q, got stage %s reason %q", tc.reason, res.Stage, res.Reason)
				}
				return
			}
			if res.Stage != StagePendingScore {
				t.Fatalf("want kept, got %s %q (%q)", res.Stage, res.Reason, res.Evidence)
			}
			found := false
			for _, c := range res.Categories {
				found = found || c == tc.category
			}
			if !found {
				t.Fatalf("categories %v missing %q", res.Categories, tc.category)
			}
		})
	}
}

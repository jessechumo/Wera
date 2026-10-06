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
			res := e.Apply(tc.title, "Chicago, IL", neutralDesc)
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
			res := e.Apply(title, location, tc.desc)
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

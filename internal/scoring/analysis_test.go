package scoring

import (
	"errors"
	"strings"
	"testing"
)

func TestStripFences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain json", `{"a":1}`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"bare fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"fence no trailing", "```json\n{\"a\":1}", `{"a":1}`},
		{"leading whitespace", "  {\"a\":1}  ", `{"a":1}`},
		{"no fence at all", "{\"a\":1}\n", `{"a":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripFences(tc.in); got != tc.want {
				t.Errorf("StripFences(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

const validReply = `{
  "fit_score": 72,
  "verdict": "good",
  "seniority": "junior",
  "years_required": 2,
  "sponsorship": "unknown",
  "sponsorship_quote": null,
  "us_eligible": true,
  "work_mode": "remote",
  "location_summary": "Remote (US)",
  "skills_matched": ["linux", "python"],
  "skills_missing": ["kubernetes"],
  "reason": "Solid match."
}`

func TestParseAnalysisValid(t *testing.T) {
	a, err := ParseAnalysis("```json\n" + validReply + "\n```")
	if err != nil {
		t.Fatalf("ParseAnalysis: %v", err)
	}
	if a.FitScore != 72 || a.Verdict != "good" || a.Seniority != "junior" {
		t.Errorf("got %+v", a)
	}
	if a.YearsRequired == nil || *a.YearsRequired != 2 {
		t.Errorf("years_required: got %v", a.YearsRequired)
	}
	if a.USEligible == nil || !*a.USEligible {
		t.Errorf("us_eligible: got %v", a.USEligible)
	}
	if len(a.SkillsMatched) != 2 || a.SkillsMatched[0] != "linux" {
		t.Errorf("skills_matched: got %v", a.SkillsMatched)
	}
}

func TestParseAnalysisInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"not json", "Sure! Here's my assessment: the job looks good."},
		{"bad verdict", strings.Replace(validReply, `"good"`, `"maybe"`, 1)},
		{"bad seniority", strings.Replace(validReply, `"junior"`, `"grizzled veteran"`, 1)},
		{"bad sponsorship", strings.Replace(validReply, `"unknown"`, `"maybe"`, 1)},
		{"bad work_mode", strings.Replace(validReply, `"remote"`, `"orbital"`, 1)},
		{"score too high", strings.Replace(validReply, `"fit_score": 72`, `"fit_score": 101`, 1)},
		{"score negative", strings.Replace(validReply, `"fit_score": 72`, `"fit_score": -1`, 1)},
		{"negative years", strings.Replace(validReply, `"years_required": 2`, `"years_required": -3`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAnalysis(tc.in)
			if !errors.Is(err, ErrInvalidSchema) {
				t.Errorf("ParseAnalysis(%s): want ErrInvalidSchema, got %v", tc.name, err)
			}
		})
	}
}

func TestParseAnalysisNullsAllowed(t *testing.T) {
	reply := `{"fit_score": 50, "verdict": "stretch", "seniority": "unknown",
	           "years_required": null, "sponsorship": "unknown",
	           "sponsorship_quote": null, "us_eligible": null,
	           "work_mode": "unknown", "location_summary": "",
	           "skills_matched": [], "skills_missing": [], "reason": ""}`
	a, err := ParseAnalysis(reply)
	if err != nil {
		t.Fatalf("ParseAnalysis: %v", err)
	}
	if a.YearsRequired != nil || a.USEligible != nil || a.SponsorshipQuote != nil {
		t.Errorf("want all nullables nil, got %+v", a)
	}
}

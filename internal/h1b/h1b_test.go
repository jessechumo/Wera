package h1b

import (
	"slices"
	"testing"
	"time"
)

func TestEmployerKey(t *testing.T) {
	for in, want := range map[string][2]string{
		"Stripe, Inc.":                      {"stripe", "stripe"},
		"STRIPE INC":                        {"stripe", "stripe"},
		"Amazon.com Services LLC":           {"amazon com services", "amazon"},
		"Meta Platforms, Inc.":              {"meta platforms", ""},
		"Bank of America, N.A.":             {"bank of america", "bank of america"},
		"The Boeing Company":                {"boeing", "boeing"},
		"AT&T Services, Inc.":               {"at and t services", "at and t"},
		"Acme Holdings LLC dba Rocket Labs": {"acme holdings", ""},
	} {
		if got := [2]string{EmployerKey(in), LooseKey(in)}; got != want {
			t.Errorf("keys(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	var keys []string
	for _, n := range []string{"Amazon.com Services LLC", "Amazon Web Services, Inc.", "Amazon Hospitality LLC", "Apple Inc.",
		"Apple Hospitality REIT", "Planet, Inc.", "Planet Labs PBC", "Compass, Inc.", "Compass Health", "Bank of America N.A.", "Bank of China", "Meta Platforms Inc"} {
		keys = append(keys, EmployerKey(n))
	}
	m := NewMatcher(keys)
	for _, tc := range []struct {
		company string
		aliases []string
		want    []string
	}{
		{"Amazon", []string{"Amazon Web Services"}, []string{"amazon com services", "amazon web services"}},
		{"Apple", nil, []string{"apple"}},
		{"Compass", nil, []string{"compass"}},
		{"Planet Labs", nil, []string{"planet labs"}},
		{"Bank of America", nil, []string{"bank of america"}},
		{"Meta", nil, nil},
		{"Meta", []string{"Meta Platforms"}, []string{"meta platforms"}},
	} {
		if got := m.Match(tc.company, tc.aliases...); !slices.Equal(got, tc.want) {
			t.Errorf("%s %v: got %v, want %v", tc.company, tc.aliases, got, tc.want)
		}
	}
}

func TestParseRow(t *testing.T) {
	h, missing := ParseHeader(needed)
	if len(missing) > 0 {
		t.Fatal(missing)
	}
	row := func(vals map[string]string) []string {
		r := make([]string, len(needed))
		for k, v := range vals {
			r[h[k]] = v
		}
		return r
	}
	base := map[string]string{
		"CASE_NUMBER": "I-200-25273-1", "CASE_STATUS": "Certified", "DECISION_DATE": "10-03-25", "VISA_CLASS": "H-1B",
		"JOB_TITLE": "Software Engineer II", "SOC_CODE": "15-1252.00", "SOC_TITLE": "Software Developers",
		"FULL_TIME_POSITION": "Y", "TOTAL_WORKER_POSITIONS": "2", "NEW_EMPLOYMENT": "1", "EMPLOYER_NAME": "Stripe, Inc.",
		"WORKSITE_CITY": "SOUTH SAN FRANCISCO", "WORKSITE_STATE": "ca", "WAGE_RATE_OF_PAY_FROM": "$75.00 ",
		"WAGE_RATE_OF_PAY_TO": "", "WAGE_UNIT_OF_PAY": "Hour", "PW_WAGE_LEVEL": "II",
	}
	c, ok := h.ParseRow(row(base))
	if !ok {
		t.Fatal("certified H-1B row skipped")
	}
	if c.FiscalYear != 2026 || !c.DecisionDate.Equal(time.Date(2025, 10, 3, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("date: %v FY%d", c.DecisionDate, c.FiscalYear)
	}
	if c.WageFrom == nil || *c.WageFrom != 156000 || c.WageTo != nil {
		t.Errorf("wage: %v %v", c.WageFrom, c.WageTo)
	}
	if c.EmployerKey != "stripe" || c.WorksiteCity != "South San Francisco" || c.WorksiteState != "CA" || c.Positions != 2 || !c.NewEmployment {
		t.Errorf("case: %+v", c)
	}
	for k, v := range map[string]string{"CASE_STATUS": "Withdrawn", "VISA_CLASS": "E-3 Australian", "DECISION_DATE": "soon"} {
		bad := map[string]string{}
		for kk, vv := range base {
			bad[kk] = vv
		}
		bad[k] = v
		if _, ok := h.ParseRow(row(bad)); ok {
			t.Errorf("%s=%s must be skipped", k, v)
		}
	}
	bad := row(base)
	bad[h["JOB_TITLE"]] = "Engineer\x80\x00 II"
	if c, _ := h.ParseRow(bad); c.JobTitle != "Engineer II" {
		t.Errorf("invalid bytes kept: %q", c.JobTitle)
	}
	if got := titleCase("ÉCOLE PARK"); got != "École Park" {
		t.Errorf("titleCase: %q", got)
	}
	if w := annual("$1.00", "Hour"); w != nil {
		t.Errorf("implausible wage kept: %v", *w)
	}
	if d, ok := parseDate("45931"); !ok || d.Format("2006-01-02") != "2025-10-01" {
		t.Errorf("excel serial: %v", d)
	}
}

// Package h1b reads the US Department of Labor's H-1B Labor Condition
// Application disclosure files and matches the filing employers to the
// companies Wera tracks.
package h1b

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Case is one certified H-1B application, as stored.
type Case struct {
	CaseNumber    string
	FiscalYear    int
	DecisionDate  time.Time
	EmployerName  string
	EmployerKey   string
	JobTitle      string
	SOCCode       string
	SOCTitle      string
	WageFrom      *float64 // per year
	WageTo        *float64
	WageLevel     string
	WorksiteCity  string
	WorksiteState string
	Positions     int
	NewEmployment bool
	FullTime      bool
}

// Columns the parser needs from the file's header row.
var needed = []string{
	"CASE_NUMBER", "CASE_STATUS", "DECISION_DATE", "VISA_CLASS", "JOB_TITLE", "SOC_CODE", "SOC_TITLE",
	"FULL_TIME_POSITION", "TOTAL_WORKER_POSITIONS", "NEW_EMPLOYMENT", "EMPLOYER_NAME",
	"WORKSITE_CITY", "WORKSITE_STATE", "WAGE_RATE_OF_PAY_FROM", "WAGE_RATE_OF_PAY_TO", "WAGE_UNIT_OF_PAY", "PW_WAGE_LEVEL",
}

// Header maps column names to their positions.
type Header map[string]int

// ParseHeader reads the header row; ok is false when a needed column is
// missing (not an LCA disclosure file, or a changed layout).
func ParseHeader(row []string) (Header, []string) {
	h := Header{}
	for i, c := range row {
		h[strings.ToUpper(strings.TrimSpace(c))] = i
	}
	var missing []string
	for _, c := range needed {
		if _, ok := h[c]; !ok {
			missing = append(missing, c)
		}
	}
	return h, missing
}

func (h Header) get(row []string, col string) string {
	i, ok := h[col]
	if !ok || i >= len(row) {
		return ""
	}
	// Some rows hold stray bytes that are not valid text.
	return strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(row[i], ""), "\x00", ""))
}

// ParseRow turns a row into a Case; ok is false for rows to skip (not a
// certified H-1B, or unreadable).
func (h Header) ParseRow(row []string) (Case, bool) {
	if h.get(row, "CASE_STATUS") != "Certified" || h.get(row, "VISA_CLASS") != "H-1B" {
		return Case{}, false
	}
	c := Case{
		CaseNumber:    h.get(row, "CASE_NUMBER"),
		EmployerName:  h.get(row, "EMPLOYER_NAME"),
		JobTitle:      h.get(row, "JOB_TITLE"),
		SOCCode:       h.get(row, "SOC_CODE"),
		SOCTitle:      h.get(row, "SOC_TITLE"),
		WageLevel:     h.get(row, "PW_WAGE_LEVEL"),
		WorksiteCity:  titleCase(h.get(row, "WORKSITE_CITY")),
		WorksiteState: strings.ToUpper(h.get(row, "WORKSITE_STATE")),
		FullTime:      h.get(row, "FULL_TIME_POSITION") != "N",
		Positions:     1,
	}
	if c.CaseNumber == "" || c.EmployerName == "" || c.JobTitle == "" {
		return Case{}, false
	}
	d, ok := parseDate(h.get(row, "DECISION_DATE"))
	if !ok {
		return Case{}, false
	}
	c.DecisionDate = d
	c.FiscalYear = FiscalYear(d)
	if n, err := strconv.Atoi(h.get(row, "TOTAL_WORKER_POSITIONS")); err == nil && n > 0 {
		c.Positions = n
	}
	if n, err := strconv.Atoi(h.get(row, "NEW_EMPLOYMENT")); err == nil && n > 0 {
		c.NewEmployment = true
	}
	unit := h.get(row, "WAGE_UNIT_OF_PAY")
	c.WageFrom = annual(h.get(row, "WAGE_RATE_OF_PAY_FROM"), unit)
	c.WageTo = annual(h.get(row, "WAGE_RATE_OF_PAY_TO"), unit)
	if c.WageTo != nil && c.WageFrom != nil && *c.WageTo < *c.WageFrom {
		c.WageTo = nil
	}
	c.EmployerKey = EmployerKey(c.EmployerName)
	return c, c.EmployerKey != ""
}

// FiscalYear is the US federal fiscal year (it starts on October 1).
func FiscalYear(d time.Time) int {
	if d.Month() >= time.October {
		return d.Year() + 1
	}
	return d.Year()
}

// parseDate reads the file's dates: "09-30-25" text, ISO dates, or
// Excel serial numbers.
func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{"01-02-06", "2006-01-02", "1/2/2006", "01/02/2006", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 30000 && n < 80000 {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n)), true
	}
	return time.Time{}, false
}

var perYear = map[string]float64{"year": 1, "month": 12, "bi-weekly": 26, "week": 52, "hour": 2080}

// annual converts "$150,000.00" in the given unit to a yearly figure.
// Implausible values (a data entry slip) are dropped.
func annual(s, unit string) *float64 {
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return nil
	}
	m, ok := perYear[strings.ToLower(strings.TrimSpace(unit))]
	if !ok {
		return nil
	}
	v *= m
	if v < 15000 || v > 2000000 {
		return nil
	}
	v = float64(int64(v + 0.5))
	return &v
}

var (
	nonWord = regexp.MustCompile(`[^a-z0-9]+`)
	// Legal forms, which never tell employers apart ("Stripe, Inc." is
	// Stripe); "com" covers names like "Amazon.com".
	legal = map[string]bool{
		"inc": true, "incorporated": true, "llc": true, "l": true, "c": true, "corp": true, "corporation": true,
		"co": true, "company": true, "ltd": true, "limited": true, "lp": true, "llp": true, "plc": true, "pc": true,
		"pllc": true, "pbc": true, "na": true, "n": true, "a": true, "com": true, "the": true,
	}
	// Generic words companies add to their filing names ("Amazon.com
	// Services", "Meta Platforms"); ignored only when comparing names.
	generic = map[string]bool{
		"services": true, "service": true, "technologies": true, "technology": true, "tech": true, "systems": true,
		"labs": true, "group": true, "holdings": true, "holding": true, "usa": true, "us": true,
		"international": true, "intl": true, "global": true, "platforms": true, "software": true,
		"ai": true, "io": true, "hq": true, "opco": true,
	}
	// A trailing word after these is part of the name ("Bank of America").
	joiners = map[string]bool{"of": true, "and": true, "for": true, "the": true}
)

// EmployerKey normalizes an employer or company name: lowercase words
// without punctuation and legal forms ("Stripe, Inc." and "STRIPE INC"
// are both "stripe").
func EmployerKey(name string) string {
	return strings.Join(trim(words(name), legal), " ")
}

// LooseKey also drops generic trailing words ("Amazon.com Services LLC"
// is "amazon"), for comparing names; it is "" when what is left is too
// short to tell companies apart.
func LooseKey(name string) string {
	k := strings.Join(trim(trim(words(name), legal), func() map[string]bool {
		m := map[string]bool{}
		for w := range legal {
			m[w] = true
		}
		for w := range generic {
			m[w] = true
		}
		return m
	}()), " ")
	if len(k) < 5 {
		return ""
	}
	return k
}

func words(name string) []string {
	s := strings.ToLower(name)
	if i := strings.Index(s, " dba "); i > 0 { // "Acme LLC dba Rocket": the legal name
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "&", " and ")
	w := strings.Fields(nonWord.ReplaceAllString(s, " "))
	for i := 1; i+2 < len(w); i++ { // "d/b/a" written with slashes
		if w[i] == "d" && w[i+1] == "b" && w[i+2] == "a" {
			return w[:i]
		}
	}
	return w
}

func trim(w []string, drop map[string]bool) []string {
	for len(w) > 1 && drop[w[len(w)-1]] && !joiners[w[len(w)-2]] {
		w = w[:len(w)-1]
	}
	for len(w) > 1 && w[0] == "the" {
		w = w[1:]
	}
	return w
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(words, " ")
}

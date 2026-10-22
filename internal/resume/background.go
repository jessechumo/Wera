package resume

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Experience is one job as application forms ask for it.
type Experience struct {
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location,omitempty"`
	Start       string `json:"start,omitempty"` // "2022-01", or "2022" when only the year is known
	End         string `json:"end,omitempty"`   // "" while Current
	Current     bool   `json:"current"`
	Description string `json:"description,omitempty"` // the bullets, one per line
}

// Education is one school as application forms ask for it.
type Education struct {
	School   string `json:"school"`
	Degree   string `json:"degree,omitempty"` // as written: "Bachelor of Science"
	Level    string `json:"level,omitempty"`  // high_school | associate | bachelor | master | doctorate
	Major    string `json:"major,omitempty"`
	Location string `json:"location,omitempty"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"` // graduation (expected), "2026-05" or "2026"
	GPA      string `json:"gpa,omitempty"`
}

// Background is what a resume tells an application form: jobs, schools,
// skills and the contact details in its header.
type Background struct {
	Experience []Experience `json:"experience"`
	Education  []Education  `json:"education"`
	Skills     []string     `json:"skills"`
	Summary    string       `json:"summary,omitempty"`
	// Contact and location from the header (application detail keys).
	Details map[string]string `json:"-"`
}

var (
	experienceTitle = regexp.MustCompile(`(?i)experience|employment|work history|career|positions|professional`)
	educationTitle  = regexp.MustCompile(`(?i)education|academic|schooling`)
	skipTitle       = regexp.MustCompile(`(?i)project|volunteer|leadership|activities|activity|award|publication|certific`)
	gpaRE           = regexp.MustCompile(`(?i)\bGPA\b[:\s]*([0-4]\.\d{1,2})(\s*/\s*4(\.0+)?)?|([0-4]\.\d{1,2})\s*/\s*4(\.0+)?\s*GPA`)
	titleWords      = regexp.MustCompile(`(?i)\b(engineer|developer|analyst|scientist|manager|intern|internship|assistant|associate|consultant|designer|nurse|technician|specialist|coordinator|director|lead|architect|administrator|officer|researcher|teacher|representative|accountant|fellow)\b`)
)

// BackgroundOf reads a structured resume. Hidden sections, entries and
// bullets are still part of the person's history, so they count.
func BackgroundOf(r *Resume, now time.Time) Background {
	b := Background{Experience: []Experience{}, Education: []Education{}, Skills: []string{}, Details: map[string]string{}}
	for _, s := range r.Sections {
		switch {
		case s.Kind == KindSummary:
			if b.Summary == "" {
				b.Summary = strings.TrimSpace(s.Text)
			}
		case s.Kind == KindSkills:
			for _, g := range s.Skills {
				for _, it := range strings.Split(g.Items, ",") {
					if it = strings.TrimSpace(it); it != "" {
						b.Skills = append(b.Skills, it)
					}
				}
			}
		case educationTitle.MatchString(s.Title):
			for _, e := range s.Entries {
				b.Education = append(b.Education, educationOf(e, s.Kind))
			}
		case experienceTitle.MatchString(s.Title) && !skipTitle.MatchString(s.Title) && (s.Kind == KindEntries || s.Kind == KindCompact):
			for _, e := range s.Entries {
				b.Experience = append(b.Experience, experienceOf(e, s.Kind))
			}
		}
	}
	// Most recent first, as forms list them (current jobs on top).
	sort.SliceStable(b.Experience, func(i, j int) bool {
		a, c := b.Experience[i], b.Experience[j]
		if a.Current != c.Current {
			return a.Current
		}
		return endKey(a.End) > endKey(c.End)
	})
	sort.SliceStable(b.Education, func(i, j int) bool { return endKey(b.Education[i].End) > endKey(b.Education[j].End) })

	d := b.Details
	if f := strings.Fields(r.Name); len(f) > 0 {
		d["first_name"] = f[0]
		if len(f) > 1 {
			d["last_name"] = f[len(f)-1]
			if len(f) > 2 {
				d["middle_name"] = strings.Join(f[1:len(f)-1], " ")
			}
		}
	}
	d["email"], d["phone"] = strings.TrimSpace(r.Email), strings.TrimSpace(r.Phone)
	city, state, country := SplitLocation(r.Location)
	d["city"], d["state"], d["country"] = city, state, country
	for _, l := range r.Links {
		u := strings.TrimSpace(l.URL)
		low := strings.ToLower(u)
		switch {
		case strings.Contains(low, "linkedin.com"):
			d["linkedin"] = u
		case strings.Contains(low, "github.com"):
			d["github"] = u
		case strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:"):
		case d["portfolio"] == "":
			d["portfolio"] = u
		}
	}
	if len(b.Experience) > 0 {
		d["current_company"], d["current_title"] = b.Experience[0].Company, b.Experience[0].Title
		if y := YearsOfExperience(b.Experience, now); y > 0 {
			d["years_experience"] = strconv.Itoa(y)
		}
	}
	if len(b.Education) > 0 {
		e := b.Education[0]
		d["school"], d["degree"], d["major"], d["gpa"] = e.School, e.Degree, e.Major, e.GPA
		if len(e.End) >= 4 {
			d["graduation_year"] = e.End[:4]
		}
	}
	for k, v := range d {
		if v == "" {
			delete(d, k)
		}
	}
	return b
}

func endKey(end string) string {
	if end == "" {
		return "9999"
	}
	return end
}

func bulletsText(e Entry) string {
	var lines []string
	for _, bl := range e.Bullets {
		if t := strings.TrimSpace(bl.Text); t != "" {
			lines = append(lines, strings.ReplaceAll(t, "**", ""))
		}
	}
	if len(lines) == 0 && e.Text != "" {
		return strings.TrimSpace(e.Text)
	}
	return strings.Join(lines, "\n")
}

func experienceOf(e Entry, kind Kind) Experience {
	company, title := e.Heading, e.Subheading
	// Compact rows read "Title | Organization"; some imports put the title first in full entries too.
	if kind == KindCompact || (titleWords.MatchString(company) && !titleWords.MatchString(title)) {
		company, title = title, company
	}
	x := Experience{Title: strings.TrimSpace(title), Company: strings.TrimSpace(company), Location: strings.TrimSpace(e.Location), Description: bulletsText(e)}
	x.Start, x.End, x.Current = ParseDates(e.Dates)
	return x
}

var (
	// "Master of Science in Business Analytics", "Bachelor's degree, Biology".
	degreeLong = regexp.MustCompile(`(?i)^((?:bachelor|master|doctor|associate)(?:'?s)?(?:\s+of\s+(?:applied\s+)?(?:science|arts|business administration|engineering|fine arts|education|nursing|laws|philosophy|technology|public health|social work|commerce|computer applications))?(?:\s+degree)?)(?:\s+(?:in|of)\s+|\s*[,:;–—-]\s*|\s+)?(.*)$`)
	// "B.S. Computer Science", "MBA, Finance", "Ph.D. in Physics".
	degreeShort = regexp.MustCompile(`(?i)^((?:b|m|a)\.?\s?(?:s|a|sc|eng|tech|ba|fa|sn|s\.n)\.?|bsc|msc|mba|bba|beng|meng|ph\.?\s?d\.?|m\.?d\.?|j\.?d\.?)(?:\s+(?:in|of)\s+|\s*[,:;–—-]\s*|\s+)(.+)$`)
	levels      = []struct {
		level string
		re    *regexp.Regexp
	}{
		{"doctorate", regexp.MustCompile(`(?i)\b(ph\.?\s?d|doctor|doctorate|d\.?phil|ed\.?d|m\.?d\.?|j\.?d\.?)\b`)},
		{"master", regexp.MustCompile(`(?i)\b(master|m\.?\s?s\.?c?|m\.?\s?a\.?|mba|m\.?eng|meng|m\.?s\.?n|mph|mfa)\b`)},
		{"bachelor", regexp.MustCompile(`(?i)\b(bachelor|b\.?\s?s\.?c?|b\.?\s?a\.?|bba|b\.?eng|beng|b\.?s\.?n|b\.?tech|undergraduate)\b`)},
		{"associate", regexp.MustCompile(`(?i)\b(associate|a\.?\s?s\.?|a\.?\s?a\.?|a\.?d\.?n)\b`)},
		{"high_school", regexp.MustCompile(`(?i)high school|secondary|ged\b|diploma`)},
	}
)

// DegreeLevel is the level of a degree as written ("B.S. in Biology" is
// "bachelor"), "" when unclear.
func DegreeLevel(degree string) string {
	for _, l := range levels {
		if l.re.MatchString(degree) {
			return l.level
		}
	}
	return ""
}

func educationOf(e Entry, kind Kind) Education {
	school, degree := e.Heading, e.Subheading
	if kind == KindCompact || (DegreeLevel(school) != "" && DegreeLevel(degree) == "") {
		school, degree = degree, school
	}
	ed := Education{School: strings.TrimSpace(school), Location: strings.TrimSpace(e.Location)}
	degree = strings.TrimSpace(degree)
	all := strings.Join([]string{degree, e.Dates, bulletsText(e)}, " ")
	if m := gpaRE.FindStringSubmatch(all); m != nil {
		ed.GPA = m[1]
		if ed.GPA == "" {
			ed.GPA = m[4]
		}
	}
	// "B.S. in Computer Science, GPA 3.8" -> degree "B.S.", major "Computer Science".
	degree = strings.TrimSpace(gpaRE.ReplaceAllString(degree, ""))
	degree = strings.Trim(degree, " ,;|–—-")
	ed.Degree = degree
	if m := degreeLong.FindStringSubmatch(degree); m != nil {
		ed.Degree, ed.Major = strings.TrimSpace(m[1]), m[2]
	} else if m := degreeShort.FindStringSubmatch(degree); m != nil {
		ed.Degree, ed.Major = strings.TrimSpace(m[1]), m[2]
	}
	ed.Major = strings.Trim(strings.TrimSpace(ed.Major), " ,;|")
	if i := strings.IndexAny(ed.Major, ",;|("); i > 0 {
		ed.Major = strings.TrimSpace(ed.Major[:i])
	}
	ed.Level = DegreeLevel(ed.Degree)
	ed.Start, ed.End, _ = ParseDates(e.Dates)
	if ed.End == "" && ed.Start != "" && !strings.Contains(strings.ToLower(e.Dates), "present") {
		// A single date on a school is its graduation ("May 2026", "Expected 2026").
		ed.End, ed.Start = ed.Start, ""
	}
	return ed
}

var (
	monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12}
	seasons    = map[string]int{"spring": 3, "summer": 6, "fall": 9, "autumn": 9, "winter": 12}
	dateTok    = regexp.MustCompile(`(?i)(present|current|now|today|ongoing)|(\d{1,2})\s*/\s*(\d{4})|(\d{4})\s*[-/.]\s*(\d{1,2})\b|([a-z]{3,9})\.?\s+(\d{4})|(\d{4})`)
)

// ParseDates reads a resume's date range ("Jan 2022 – Present", "2019 -
// 2023", "05/2021 – 08/2021", "Summer 2021") into start and end as
// "YYYY-MM" (or "YYYY"), and whether it runs to the present.
func ParseDates(s string) (start, end string, current bool) {
	var got []string
	for _, m := range dateTok.FindAllStringSubmatch(s, -1) {
		switch {
		case m[1] != "":
			current = true
		case m[2] != "":
			got = append(got, ym(m[3], m[2]))
		case m[4] != "":
			got = append(got, ym(m[4], m[5]))
		case m[6] != "":
			w := strings.ToLower(m[6])
			if n, ok := monthNames[w[:3]]; ok {
				got = append(got, ym(m[7], strconv.Itoa(n)))
			} else if n, ok := seasons[w]; ok {
				got = append(got, ym(m[7], strconv.Itoa(n)))
			} else {
				got = append(got, m[7])
			}
		case m[8] != "":
			got = append(got, m[8])
		}
	}
	if len(got) > 0 {
		start = got[0]
	}
	if len(got) > 1 && !current {
		end = got[len(got)-1]
	}
	return start, end, current
}

func ym(year, month string) string {
	n, _ := strconv.Atoi(month)
	if n < 1 || n > 12 {
		return year
	}
	return year + "-" + pad2(n)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func monthIndex(d string, endOfYear bool) int {
	if len(d) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(d[:4])
	m := 1
	if endOfYear {
		m = 12
	}
	if len(d) >= 7 {
		m, _ = strconv.Atoi(d[5:7])
	}
	return y*12 + m - 1
}

// YearsOfExperience counts the months the jobs cover (overlaps once),
// in whole years.
func YearsOfExperience(xs []Experience, now time.Time) int {
	type span struct{ a, b int }
	var spans []span
	nowIdx := now.Year()*12 + int(now.Month()) - 1
	for _, x := range xs {
		if x.Start == "" {
			continue
		}
		a := monthIndex(x.Start, false)
		b := nowIdx
		if !x.Current {
			if x.End == "" {
				continue
			}
			b = monthIndex(x.End, true)
		}
		if b >= a {
			spans = append(spans, span{a, b + 1})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	total, curA, curB := 0, -1, -1
	for _, s := range spans {
		if s.a > curB {
			total += curB - curA
			curA, curB = s.a, s.b
		} else if s.b > curB {
			curB = s.b
		}
	}
	total += curB - curA
	return total / 12
}

var usStates = map[string]bool{"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true, "CO": true, "CT": true, "DE": true, "DC": true, "FL": true, "GA": true, "HI": true, "ID": true, "IL": true, "IN": true, "IA": true, "KS": true, "KY": true, "LA": true, "ME": true, "MD": true, "MA": true, "MI": true, "MN": true, "MS": true, "MO": true, "MT": true, "NE": true, "NV": true, "NH": true, "NJ": true, "NM": true, "NY": true, "NC": true, "ND": true, "OH": true, "OK": true, "OR": true, "PA": true, "RI": true, "SC": true, "SD": true, "TN": true, "TX": true, "UT": true, "VT": true, "VA": true, "WA": true, "WV": true, "WI": true, "WY": true}

// SplitLocation reads "Dallas, TX", "Austin, Texas, USA" or "Nairobi,
// Kenya" into city, state and country ("United States" for a US state).
func SplitLocation(loc string) (city, state, country string) {
	var parts []string
	for _, p := range strings.Split(loc, ",") {
		if p = strings.TrimSpace(p); p != "" && !strings.EqualFold(p, "remote") {
			parts = append(parts, p)
		}
	}
	switch len(parts) {
	case 0:
		return "", "", ""
	case 1:
		return parts[0], "", ""
	case 2:
		if s := strings.ToUpper(strings.Fields(parts[1])[0]); usStates[s] {
			return parts[0], s, "United States"
		}
		if isUS(parts[1]) {
			return parts[0], "", "United States"
		}
		if len(parts[1]) > 3 && stateCode(parts[1]) != "" {
			return parts[0], stateCode(parts[1]), "United States"
		}
		return parts[0], "", parts[1]
	default:
		c := parts[len(parts)-1]
		if isUS(c) {
			c = "United States"
		}
		return parts[0], parts[1], c
	}
}

func isUS(s string) bool {
	switch strings.ToLower(strings.Trim(s, ". ")) {
	case "usa", "us", "u.s", "u.s.a", "united states", "united states of america":
		return true
	}
	return false
}

var stateNames = map[string]string{"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR", "california": "CA", "colorado": "CO", "connecticut": "CT", "delaware": "DE", "florida": "FL", "georgia": "GA", "hawaii": "HI", "idaho": "ID", "illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS", "kentucky": "KY", "louisiana": "LA", "maine": "ME", "maryland": "MD", "massachusetts": "MA", "michigan": "MI", "minnesota": "MN", "mississippi": "MS", "missouri": "MO", "montana": "MT", "nebraska": "NE", "nevada": "NV", "new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY", "north carolina": "NC", "north dakota": "ND", "ohio": "OH", "oklahoma": "OK", "oregon": "OR", "pennsylvania": "PA", "rhode island": "RI", "south carolina": "SC", "south dakota": "SD", "tennessee": "TN", "texas": "TX", "utah": "UT", "vermont": "VT", "virginia": "VA", "washington": "WA", "west virginia": "WV", "wisconsin": "WI", "wyoming": "WY"}

func stateCode(name string) string { return stateNames[strings.ToLower(strings.TrimSpace(name))] }

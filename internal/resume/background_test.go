package resume

import (
	"reflect"
	"testing"
	"time"
)

func TestParseDates(t *testing.T) {
	for _, c := range []struct {
		in, start, end string
		current        bool
	}{
		{"Jan 2022 – Present", "2022-01", "", true},
		{"January 2020 - May 2021", "2020-01", "2021-05", false},
		{"2019 – 2023", "2019", "2023", false},
		{"05/2021 – 08/2021", "2021-05", "2021-08", false},
		{"Summer 2021", "2021-06", "", false},
		{"Sept. 2018 – Dec. 2019", "2018-09", "2019-12", false},
		{"Expected May 2026", "2026-05", "", false},
		{"2023-06 to current", "2023-06", "", true},
		{"", "", "", false},
	} {
		s, e, cur := ParseDates(c.in)
		if s != c.start || e != c.end || cur != c.current {
			t.Errorf("ParseDates(%q) = %q %q %v, want %q %q %v", c.in, s, e, cur, c.start, c.end, c.current)
		}
	}
}

func TestSplitLocation(t *testing.T) {
	for in, want := range map[string][3]string{
		"Dallas, TX":           {"Dallas", "TX", "United States"},
		"Austin, Texas":        {"Austin", "TX", "United States"},
		"Austin, Texas, USA":   {"Austin", "Texas", "United States"},
		"Nairobi, Kenya":       {"Nairobi", "", "Kenya"},
		"Seattle, WA 98101":    {"Seattle", "WA", "United States"},
		"Remote":               {"", "", ""},
		"New York, NY, Remote": {"New York", "NY", "United States"},
	} {
		c, s, k := SplitLocation(in)
		if [3]string{c, s, k} != want {
			t.Errorf("SplitLocation(%q) = %q %q %q, want %v", in, c, s, k, want)
		}
	}
}

func TestDegreeLevel(t *testing.T) {
	for in, want := range map[string]string{
		"Bachelor of Science": "bachelor", "B.S.": "bachelor", "BSc": "bachelor", "BA": "bachelor", "B.S.N.": "bachelor",
		"Master of Science": "master", "M.S.": "master", "MBA": "master", "Ph.D.": "doctorate", "Associate of Arts": "associate",
		"High School Diploma": "high_school", "Coursework": "",
	} {
		if got := DegreeLevel(in); got != want {
			t.Errorf("DegreeLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBackgroundOf(t *testing.T) {
	r := &Resume{
		Name: "Jesse K Chumo", Email: "jesse@example.com", Phone: "(469) 555-0101", Location: "Dallas, TX",
		Links: []Link{{Label: "linkedin.com/in/jesse", URL: "https://linkedin.com/in/jesse"}, {Label: "github.com/jesse", URL: "https://github.com/jesse"}, {Label: "jesse.dev", URL: "https://jesse.dev"}},
		Sections: []Section{
			{Kind: KindSummary, Title: "Summary", Text: "Data scientist."},
			{Kind: KindEntries, Title: "Education", Entries: []Entry{
				{Heading: "University of Texas at Dallas", Subheading: "Master of Science in Business Analytics, GPA: 3.9/4.0", Dates: "Aug 2022 – May 2024", Location: "Richardson, TX"},
				{Heading: "Moi University", Subheading: "B.S. Computer Science", Dates: "2019"},
			}},
			{Kind: KindEntries, Title: "Professional Experience", Entries: []Entry{
				{Heading: "Acme Analytics", Subheading: "Data Analyst", Dates: "Jun 2020 – Jul 2022", Location: "Nairobi, Kenya", Bullets: []Bullet{{Text: "Built **dashboards**"}, {Text: "Cut costs 10%"}}},
				{Heading: "Data Scientist", Subheading: "Globex", Dates: "Jun 2024 – Present", Location: "Dallas, TX"},
			}},
			{Kind: KindEntries, Title: "Projects", Entries: []Entry{{Heading: "Side project", Dates: "2023"}}},
			{Kind: KindSkills, Title: "Skills", Skills: []SkillGroup{{Name: "Languages", Items: "Python, SQL"}, {Name: "Tools", Items: "Tableau"}}},
		},
	}
	b := BackgroundOf(r, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	wantExp := []Experience{
		{Title: "Data Scientist", Company: "Globex", Location: "Dallas, TX", Start: "2024-06", Current: true},
		{Title: "Data Analyst", Company: "Acme Analytics", Location: "Nairobi, Kenya", Start: "2020-06", End: "2022-07", Description: "Built dashboards\nCut costs 10%"},
	}
	if !reflect.DeepEqual(b.Experience, wantExp) {
		t.Errorf("experience:\n got %+v\nwant %+v", b.Experience, wantExp)
	}
	wantEdu := []Education{
		{School: "University of Texas at Dallas", Degree: "Master of Science", Level: "master", Major: "Business Analytics", Location: "Richardson, TX", Start: "2022-08", End: "2024-05", GPA: "3.9"},
		{School: "Moi University", Degree: "B.S.", Level: "bachelor", Major: "Computer Science", End: "2019"},
	}
	if !reflect.DeepEqual(b.Education, wantEdu) {
		t.Errorf("education:\n got %+v\nwant %+v", b.Education, wantEdu)
	}
	if !reflect.DeepEqual(b.Skills, []string{"Python", "SQL", "Tableau"}) || b.Summary != "Data scientist." {
		t.Errorf("skills/summary: %v %q", b.Skills, b.Summary)
	}
	want := map[string]string{
		"first_name": "Jesse", "middle_name": "K", "last_name": "Chumo", "email": "jesse@example.com", "phone": "(469) 555-0101",
		"city": "Dallas", "state": "TX", "country": "United States", "linkedin": "https://linkedin.com/in/jesse", "github": "https://github.com/jesse",
		"portfolio": "https://jesse.dev", "current_company": "Globex", "current_title": "Data Scientist", "years_experience": "4",
		"school": "University of Texas at Dallas", "degree": "Master of Science", "major": "Business Analytics", "graduation_year": "2024", "gpa": "3.9",
	}
	if !reflect.DeepEqual(b.Details, want) {
		t.Errorf("details:\n got %v\nwant %v", b.Details, want)
	}
}

func TestYearsOfExperienceCountsOverlapOnce(t *testing.T) {
	xs := []Experience{{Start: "2020-01", End: "2021-12"}, {Start: "2021-01", End: "2022-12"}, {Start: "2019"}}
	if got := YearsOfExperience(xs, time.Now()); got != 3 {
		t.Errorf("got %d years, want 3", got)
	}
}

func TestDegreeAndMajor(t *testing.T) {
	for in, want := range map[string][3]string{
		"Bachelor of Science in Nursing (BSN)":  {"Bachelor of Science", "Nursing", "bachelor"},
		"Bachelor's degree in Biology":          {"Bachelor's degree", "Biology", "bachelor"},
		"MBA, Finance":                          {"MBA", "Finance", "master"},
		"Ph.D. in Physics":                      {"Ph.D.", "Physics", "doctorate"},
		"B.Tech Computer Engineering":           {"B.Tech", "Computer Engineering", "bachelor"},
		"Associate of Applied Science, Nursing": {"Associate of Applied Science", "Nursing", "associate"},
		"High School Diploma":                   {"High School Diploma", "", "high_school"},
		"Master of Science":                     {"Master of Science", "", "master"},
	} {
		e := educationOf(Entry{Heading: "Some University", Subheading: in}, KindEntries)
		if got := [3]string{e.Degree, e.Major, e.Level}; got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

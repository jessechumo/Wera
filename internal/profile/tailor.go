package profile

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"wera/internal/scoring"
)

const tailorInstructions = `You tailor the candidate's resume to one job posting. ` + scoring.Untrusted + `

Return ONLY a JSON object:
{"headline": "", "summary": "", "skills": [{"group": "", "items": [""]}], "experience": [{"title": "", "company": "", "dates": "", "bullets": [""]}], "projects": [{"name": "", "bullets": [""]}], "education": [""], "changes": [""], "missing_keywords": [""]}

Rules:
- Use only what the resume says. Never invent employers, titles, dates, degrees, tools, metrics or achievements. Keep every number exactly as written in the resume.
- headline: the candidate's target title for this posting, truthful to their background.
- summary: 2 or 3 sentences connecting their real experience to this role.
- skills: the resume's skills, grouped, most relevant to the posting first; only skills the resume shows.
- experience and projects: keep each role, most relevant first within the resume's order; rewrite each bullet to lead with what matters for this posting, using the posting's terms where they truthfully apply. 2 to 5 bullets per role.
- education: one line per entry, as on the resume.
- changes: 3 to 6 short notes on what you changed and why.
- missing_keywords: important posting requirements the resume does not show (so the candidate can decide; do not add them elsewhere).
- No em dashes or en dashes.`

// TailoredResume is a resume rewritten toward one posting.
type TailoredResume struct {
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
	Skills   []struct {
		Group string   `json:"group"`
		Items []string `json:"items"`
	} `json:"skills"`
	Experience []struct {
		Title   string   `json:"title"`
		Company string   `json:"company"`
		Dates   string   `json:"dates"`
		Bullets []string `json:"bullets"`
	} `json:"experience"`
	Projects []struct {
		Name    string   `json:"name"`
		Bullets []string `json:"bullets"`
	} `json:"projects"`
	Education       []string `json:"education"`
	Changes         []string `json:"changes"`
	MissingKeywords []string `json:"missing_keywords"`
	// Dropped lists bullets removed because they cited numbers the resume
	// does not contain.
	Dropped []string `json:"dropped"`
}

// TailorMessages builds the request.
func TailorMessages(resume, profileText, company, title, description string) []scoring.Message {
	cut := func(s string, n int) string {
		if r := []rune(s); len(r) > n {
			return string(r[:n])
		}
		return s
	}
	body := fmt.Sprintf("<resume>\n%s\n</resume>\n\n<profile>\n%s\n</profile>\n\n<posting>\n%s\n</posting>",
		scoring.Fence(cut(resume, 12000)), scoring.Fence(cut(profileText, 6000)),
		scoring.Fence(cut(fmt.Sprintf("Company: %s\nTitle: %s\n\n%s", company, title, description), 8000)))
	return []scoring.Message{
		{Role: "system", Content: tailorInstructions},
		{Role: "user", Content: body},
	}
}

var numberRE = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// ParseTailored reads the reply and enforces the no-invention rule where
// it can be checked: a bullet citing a number that never appears in the
// resume is dropped (and reported), as is any dash the model used.
func ParseTailored(reply, resume string) (*TailoredResume, error) {
	var t TailoredResume
	if err := json.Unmarshal([]byte(scoring.StripFences(reply)), &t); err != nil {
		return nil, fmt.Errorf("unreadable tailored resume: %w", err)
	}
	known := map[string]bool{}
	for _, n := range numberRE.FindAllString(resume, -1) {
		known[strings.ReplaceAll(n, ",", "")] = true
	}
	ok := func(s string) bool {
		for _, n := range numberRE.FindAllString(s, -1) {
			if !known[strings.ReplaceAll(n, ",", "")] {
				return false
			}
		}
		return true
	}
	t.Dropped = []string{}
	keep := func(bullets []string) []string {
		out := []string{}
		for _, b := range bullets {
			b = CleanLetter(b)
			if ok(b) {
				out = append(out, b)
			} else {
				t.Dropped = append(t.Dropped, b)
			}
		}
		return out
	}
	for i := range t.Experience {
		t.Experience[i].Bullets = keep(t.Experience[i].Bullets)
	}
	for i := range t.Projects {
		t.Projects[i].Bullets = keep(t.Projects[i].Bullets)
	}
	t.Headline, t.Summary = CleanLetter(t.Headline), CleanLetter(t.Summary)
	if !ok(t.Summary) {
		t.Dropped = append(t.Dropped, t.Summary)
		t.Summary = ""
	}
	return &t, nil
}

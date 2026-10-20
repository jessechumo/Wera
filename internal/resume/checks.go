package resume

import (
	"fmt"
	"regexp"
	"strings"
)

// Check is one finding of the resume review, pointing at what to fix.
type Check struct {
	ID       string `json:"id"`       // kind of finding, e.g. "weak-opener"
	Severity string `json:"severity"` // "fix" (hurts the resume), "improve", or "tip"
	Message  string `json:"message"`
	Target   string `json:"target,omitempty"` // bullet, entry or section id; "" for the header or the whole resume
}

// Review is the result of reviewing a resume: findings and a 0-100 score.
type Review struct {
	Score  int     `json:"score"`
	Checks []Check `json:"checks"`
}

var (
	weakOpeners = []string{"responsible for", "worked on", "helped", "assisted", "participated in", "involved in",
		"tasked with", "duties included", "in charge of", "worked with", "was part of", "contributed to"}
	// Case-aware so "US Patent" and "i.e." are not pronouns.
	pronounRE = regexp.MustCompile(`(?:^|[\s(])(I|me|my|My|we|We|our|Our|us)(?:[\s,;:)]|$)`)
	numberRE  = regexp.MustCompile(`\d`)
	cliches   = []string{"synergy", "go-getter", "hard worker", "hardworking", "team player", "detail-oriented", "detail oriented",
		"results-driven", "results driven", "self-starter", "self starter", "passionate", "think outside the box", "dynamic"}
	dateStyles = map[string]*regexp.Regexp{
		"Mon YYYY":   regexp.MustCompile(`\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Sept|Oct|Nov|Dec)\.? \d{4}\b`),
		"Month YYYY": regexp.MustCompile(`\b(January|February|March|April|June|July|August|September|October|November|December) \d{4}\b`),
		"MM/YYYY":    regexp.MustCompile(`\b\d{1,2}/\d{4}\b`),
		"YYYY-MM":    regexp.MustCompile(`\b\d{4}-\d{2}\b`),
	}
	// Section names applicant tracking systems recognize.
	standardTitles = []string{"experience", "work experience", "professional experience", "employment", "education", "skills",
		"technical skills", "projects", "summary", "professional summary", "certifications", "awards", "honors", "publications",
		"leadership", "volunteer", "activities", "research", "additional experience", "relevant experience", "languages",
		"interests", "coursework", "patents", "talks", "selected honors", "invited talks", "achievements", "profile",
		"licenses", "licensure", "licenses and certifications", "clinical experience", "clinical rotations", "teaching experience"}
)

const (
	maxBulletChars = 230 // about two lines in the templates
	minBulletChars = 35
)

func firstWord(s string) string {
	s = strings.TrimLeft(strings.ReplaceAll(s, "**", ""), " -•")
	w, _, _ := strings.Cut(s, " ")
	return strings.ToLower(strings.Trim(w, ".,;:"))
}

// Review checks the visible resume for common problems recruiters and
// applicant tracking systems care about. pages is the rendered page count
// (0 when unknown).
func (r *Resume) Review(pages int) Review {
	var checks []Check
	add := func(id, sev, target, msg string, args ...any) {
		checks = append(checks, Check{ID: id, Severity: sev, Target: target, Message: fmt.Sprintf(msg, args...)})
	}

	// Header.
	if r.Email == "" {
		add("no-email", "fix", "", "Add your email address to the header.")
	}
	if r.Phone == "" {
		add("no-phone", "improve", "", "Add a phone number: many recruiters still call first.")
	}
	hasProfile := false
	for _, l := range r.Links {
		u := strings.ToLower(l.URL)
		if strings.Contains(u, "linkedin.com") || strings.Contains(u, "github.com") {
			hasProfile = true
		}
	}
	if !hasProfile {
		add("no-profile-link", "tip", "", "Add a LinkedIn or GitHub link; reviewers often check them.")
	}
	if pages > 1 {
		add("too-long", "fix", "", "The resume runs to %d pages. Use Fit to one page, or hide older bullets.", pages)
	}

	var bulletsTotal, withNumbers, endsWithPeriod, noPeriod int
	openers := map[string][]string{}
	styles := map[string]bool{}
	hasExperience := false
	for _, s := range r.Sections {
		if s.Hidden {
			continue
		}
		title := strings.ToLower(strings.TrimSpace(s.Title))
		if strings.Contains(title, "experience") || strings.Contains(title, "employment") {
			hasExperience = true
		}
		standard := false
		for _, t := range standardTitles {
			if title == t {
				standard = true
				break
			}
		}
		if !standard && title != "" {
			add("odd-title", "tip", s.ID, "%q is not a standard section name; applicant tracking systems may not file it correctly.", s.Title)
		}
		if s.Kind == KindSummary && len(s.Text) > 450 {
			add("long-summary", "improve", s.ID, "Keep the summary to two or three lines; recruiters skim it in seconds.")
		}
		for _, g := range s.Skills {
			if n := len(splitItems(g.Items)); n > 16 {
				add("long-skills", "tip", s.ID, "%q lists %d items. Keep the ones this kind of job asks for.", g.Name, n)
			}
		}
		for _, e := range s.Entries {
			if e.Hidden {
				continue
			}
			for name, re := range dateStyles {
				if re.MatchString(e.Dates) {
					styles[name] = true
				}
			}
			if s.Kind == KindEntries && e.Dates == "" {
				add("no-dates", "improve", e.ID, "Add dates to %s.", quoteName(e.Heading))
			}
			shown := 0
			for _, b := range e.Bullets {
				if b.Hidden {
					continue
				}
				shown++
				bulletsTotal++
				text := strings.ReplaceAll(b.Text, "**", "")
				lower := strings.ToLower(text)
				if numberRE.MatchString(text) {
					withNumbers++
				}
				if strings.HasSuffix(text, ".") {
					endsWithPeriod++
				} else {
					noPeriod++
				}
				for _, w := range weakOpeners {
					if strings.HasPrefix(lower, w) {
						add("weak-opener", "improve", b.ID, "Starts with %q. Lead with a strong verb and what changed (Built, Cut, Led, Shipped).", text[:len(w)])
						break
					}
				}
				if m := pronounRE.FindStringSubmatch(text); m != nil {
					add("pronoun", "improve", b.ID, "Drop %q: resume bullets are written without I, my or we.", m[1])
				}
				if len(text) > maxBulletChars {
					add("long-bullet", "improve", b.ID, "This bullet runs past two lines. Cut it to the action and the result.")
				} else if len(text) < minBulletChars && s.Kind == KindEntries && !strings.Contains(title, "education") {
					add("short-bullet", "tip", b.ID, "Short bullet: say what it achieved or how much.")
				}
				for _, c := range cliches {
					if strings.Contains(lower, c) {
						add("cliche", "tip", b.ID, "%q is a cliché that reviewers skip; show it with a result instead.", c)
						break
					}
				}
				if w := firstWord(text); w != "" {
					openers[w] = append(openers[w], b.ID)
				}
			}
			if shown > 6 {
				add("many-bullets", "tip", e.ID, "%s has %d bullets; three to five strong ones read better.", quoteName(e.Heading), shown)
			}
		}
	}
	if !hasExperience {
		add("no-experience", "improve", "", "Add an Experience section (internships and research count).")
	}
	if bulletsTotal >= 4 && withNumbers*100/bulletsTotal < 40 {
		add("few-metrics", "improve", "", "Only %d of %d bullets have a number. Add scale or results: users, latency, %%, $, time saved.", withNumbers, bulletsTotal)
	}
	if endsWithPeriod > 0 && noPeriod > 0 && min(endsWithPeriod, noPeriod) >= 2 {
		add("periods", "tip", "", "Some bullets end with a period and some don't; pick one style.")
	}
	if len(styles) > 1 {
		add("date-styles", "tip", "", "Dates use more than one format; use one (e.g. Aug 2024 – May 2026).")
	}
	for w, ids := range openers {
		if len(ids) >= 3 && len(w) > 2 {
			add("repeated-verb", "tip", ids[2], "%d bullets start with %q; vary the verbs.", len(ids), w)
		}
	}

	score := 100
	for _, c := range checks {
		switch c.Severity {
		case "fix":
			score -= 12
		case "improve":
			score -= 5
		default:
			score -= 2
		}
	}
	if checks == nil {
		checks = []Check{}
	}
	return Review{Score: max(score, 0), Checks: checks}
}

func quoteName(s string) string {
	if s == "" {
		return "this entry"
	}
	return s
}

package resume

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"wera/internal/scoring"
)

// --- import from resume text ------------------------------------------------

const importInstructions = `You convert the text of a resume into JSON, copying the candidate's words exactly. ` + scoring.Untrusted + `

Return ONLY a JSON object:
{"name": "", "phone": "", "email": "", "location": "", "links": [{"label": "", "url": ""}],
 "sections": [{"kind": "entries"|"skills"|"projects"|"compact"|"summary", "title": "",
   "entries": [{"heading": "", "subheading": "", "dates": "", "location": "", "tech": "", "text": "", "bullets": [{"text": ""}]}],
   "skills": [{"name": "", "items": ""}], "text": ""}]}

Rules:
- Copy every line verbatim. Never rephrase, shorten, merge or invent anything.
- Keep the resume's own section order and titles.
- entries (experience, education): heading = organization or school, subheading = job title or degree, plus dates, location and bullets.
- projects: heading = project name, tech = its stack, text = its description.
- compact: one-line roles (heading = title, subheading = organization, dates).
- skills: one group per labeled line ("Languages: Python, Go" -> name "Languages", items "Python, Go").
- Links: full URLs (add https:// when the resume omits it).`

// ImportMessages builds the request that structures resume text.
func ImportMessages(text string) []scoring.Message {
	if r := []rune(text); len(r) > 16000 {
		text = string(r[:16000])
	}
	return []scoring.Message{
		{Role: "system", Content: importInstructions},
		{Role: "user", Content: "<resume>\n" + scoring.Fence(text) + "\n</resume>"},
	}
}

var looseRE = regexp.MustCompile(`[^a-z0-9]+`)

func loose(s string) string {
	return strings.TrimSpace(looseRE.ReplaceAllString(strings.ToLower(s), " "))
}

// ParseImport reads the reply into a Resume. Bullets that do not appear in
// the source text (the model reworded them) are listed in warnings.
func ParseImport(reply, source string) (*Resume, []string, error) {
	var r Resume
	if err := json.Unmarshal([]byte(scoring.StripFences(reply)), &r); err != nil {
		return nil, nil, fmt.Errorf("unreadable resume: %w", err)
	}
	for i := range r.Links {
		if u := r.Links[i].URL; u != "" && !strings.Contains(u, "://") && !strings.HasPrefix(u, "mailto:") {
			r.Links[i].URL = "https://" + u
		}
	}
	src := loose(source)
	var warnings []string
	for _, s := range r.Sections {
		for _, e := range s.Entries {
			for _, b := range e.Bullets {
				if t := loose(b.Text); t != "" && !strings.Contains(src, t) {
					warnings = append(warnings, fmt.Sprintf("Check this line, it is not word for word from your resume: %q", b.Text))
				}
			}
		}
	}
	if err := r.Normalize(); err != nil {
		return nil, warnings, err
	}
	return &r, warnings, nil
}

// --- tailoring ----------------------------------------------------------------

const tailorInstructions = `You tailor a resume to one job posting by editing it in place. ` + scoring.Untrusted + `

The resume is listed with ids: [b:ID] bullets, [g:ID] skill groups. Return ONLY a JSON object:
{"bullets": {"ID": "rewritten bullet"}, "hide": ["ID"], "skills": {"ID": "reordered items"}, "notes": [""]}

Rules:
- Rewrite only bullets that become clearly stronger for this posting: lead with what the posting values and use its terms where they truthfully apply. Leave the rest out of "bullets".
- Never invent facts: no new tools, employers, titles, dates, metrics or achievements. Keep every number exactly as in that bullet.
- "hide": bullets irrelevant to this posting (never a role's only bullet).
- "skills": the same items of a group in a better order for this posting (most relevant first). Do not add or remove items.
- "notes": 2 to 5 short notes for the candidate on what you changed, and important posting requirements their resume does not show.
- No em dashes or en dashes.`

// TailorMessages lists the resume with ids next to the posting.
func TailorMessages(r *Resume, company, title, description string, missing []string) []scoring.Message {
	var b strings.Builder
	for _, s := range r.Sections {
		if s.Hidden {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", s.Title)
		for _, g := range s.Skills {
			fmt.Fprintf(&b, "[g:%s] %s: %s\n", g.ID, g.Name, g.Items)
		}
		for _, e := range s.Entries {
			if e.Hidden {
				continue
			}
			fmt.Fprintf(&b, "%s | %s | %s %s\n", e.Heading, e.Subheading, e.Tech, e.Text)
			for _, bl := range e.Bullets {
				fmt.Fprintf(&b, "[b:%s] %s\n", bl.ID, bl.Text)
			}
		}
	}
	desc := description
	if r := []rune(desc); len(r) > 8000 {
		desc = string(r[:8000])
	}
	body := fmt.Sprintf("<resume>\n%s</resume>\n\nPosting keywords the resume does not show yet: %s\n\n<posting>\n%s\n</posting>",
		scoring.Fence(b.String()), scoring.Fence(strings.Join(missing, ", ")),
		scoring.Fence(fmt.Sprintf("Company: %s\nTitle: %s\n\n%s", company, title, desc)))
	return []scoring.Message{
		{Role: "system", Content: tailorInstructions},
		{Role: "user", Content: body},
	}
}

var numRE = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

func numbers(s string) map[string]bool {
	m := map[string]bool{}
	for _, n := range numRE.FindAllString(s, -1) {
		m[strings.ReplaceAll(n, ",", "")] = true
	}
	return m
}

func splitItems(s string) []string {
	// Split on commas outside parentheses: "AWS (S3, EC2), Docker".
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// ApplyTailoring applies a tailoring reply to a copy of the base resume,
// enforcing what can be checked: rewrites may not add numbers, skill
// groups may only be reordered, and every role keeps a bullet. Rejected
// edits are reported in notes.
func ApplyTailoring(base *Resume, reply string) (*Resume, []string, error) {
	var t struct {
		Bullets map[string]string `json:"bullets"`
		Hide    []string          `json:"hide"`
		Skills  map[string]string `json:"skills"`
		Notes   []string          `json:"notes"`
	}
	if err := json.Unmarshal([]byte(scoring.StripFences(reply)), &t); err != nil {
		return nil, nil, fmt.Errorf("unreadable tailoring: %w", err)
	}
	raw, _ := json.Marshal(base)
	var r Resume
	_ = json.Unmarshal(raw, &r) // deep copy
	notes := []string{}
	for _, n := range t.Notes {
		if n = strings.TrimSpace(cleanDashes(n)); n != "" && len(notes) < 6 {
			notes = append(notes, n)
		}
	}
	hide := map[string]bool{}
	for _, id := range t.Hide {
		hide[id] = true
	}
	rejected := 0
	for si := range r.Sections {
		s := &r.Sections[si]
		for gi := range s.Skills {
			g := &s.Skills[gi]
			next, ok := t.Skills[g.ID]
			if !ok {
				continue
			}
			have := map[string]bool{}
			for _, it := range splitItems(g.Items) {
				have[strings.ToLower(it)] = true
			}
			items := splitItems(next)
			valid := len(items) == len(have)
			for _, it := range items {
				valid = valid && have[strings.ToLower(it)]
			}
			if valid {
				g.Items = strings.Join(items, ", ")
			} else {
				rejected++
			}
		}
		for ei := range s.Entries {
			e := &s.Entries[ei]
			for bi := range e.Bullets {
				b := &e.Bullets[bi]
				if text, ok := t.Bullets[b.ID]; ok {
					text = strings.TrimSpace(cleanDashes(text))
					okNums := true
					orig := numbers(b.Text)
					for n := range numbers(text) {
						okNums = okNums && orig[n]
					}
					if okNums && text != "" && len(text) <= maxBullet {
						b.Text = text
					} else {
						rejected++
					}
				}
				if hide[b.ID] {
					b.Hidden = true
				}
			}
			// Never leave a shown role with no bullets.
			shown := 0
			for _, b := range e.Bullets {
				if !b.Hidden {
					shown++
				}
			}
			if shown == 0 && len(e.Bullets) > 0 {
				e.Bullets[0].Hidden = false
			}
		}
	}
	if rejected > 0 {
		notes = append(notes, fmt.Sprintf("Kept your original wording in %d place%s where the suggested edit added facts.", rejected, plural(rejected)))
	}
	return &r, notes, r.Normalize()
}

func cleanDashes(s string) string {
	return strings.NewReplacer(" — ", ", ", "—", ", ", " – ", ", ").Replace(s)
}

// KeywordPriority keeps bullets that mention the posting's keywords: each
// hit counts more than the default ordering.
func KeywordPriority(keywords []string) Priority {
	return func(si, ei, bi int, s Section, b Bullet) float64 {
		hits := len(KeywordCoverage(b.Text, keywords).Matched)
		return DefaultPriority(si, ei, bi, s, b) + float64(hits)*400
	}
}

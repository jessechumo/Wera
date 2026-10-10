package profile

import (
	"fmt"
	"strings"

	"wera/internal/config"
	"wera/internal/scoring"
)

// Answers are the signup questionnaire. Everything is optional; the more a
// user fills in, the better the drafted profile.
type Answers struct {
	CurrentTitle      string   `json:"current_title"`
	YearsExperience   *float64 `json:"years_experience"`
	WorkAuthorization string   `json:"work_authorization"` // see workAuthText
	Locations         string   `json:"locations"`
	WorkModes         []string `json:"work_modes"` // remote | hybrid | onsite
	Industries        []string `json:"industries"` // industries.yaml ids
	TargetRoles       string   `json:"target_roles"`
	Avoid             string   `json:"avoid"`
	Notes             string   `json:"notes"`
}

// workAuthText explains each work-authorization choice to the LLM.
var workAuthText = map[string]string{
	"citizen_or_resident": "U.S. citizen or permanent resident; does not need visa sponsorship.",
	"sponsorship_now":     "Needs employer visa sponsorship (e.g. H-1B) to work in the U.S. now.",
	"sponsorship_future":  "Authorized to work in the U.S. now (e.g. on OPT/STEM OPT) but will need visa sponsorship (e.g. H-1B) in the future.",
	"outside_us":          "Based outside the U.S.",
}

// Validate checks field sizes and enumerations.
func (a *Answers) Validate(industries map[string]bool) error {
	for name, v := range map[string]string{
		"current_title": a.CurrentTitle, "locations": a.Locations,
		"target_roles": a.TargetRoles, "avoid": a.Avoid,
	} {
		if len(v) > 500 {
			return fmt.Errorf("%s is too long (500 characters max)", name)
		}
	}
	if len(a.Notes) > 2000 {
		return fmt.Errorf("notes are too long (2000 characters max)")
	}
	if a.YearsExperience != nil && (*a.YearsExperience < 0 || *a.YearsExperience > 60) {
		return fmt.Errorf("years of experience must be between 0 and 60")
	}
	if a.WorkAuthorization != "" && workAuthText[a.WorkAuthorization] == "" {
		return fmt.Errorf("unknown work authorization %q", a.WorkAuthorization)
	}
	for _, m := range a.WorkModes {
		if m != "remote" && m != "hybrid" && m != "onsite" {
			return fmt.Errorf("unknown work mode %q", m)
		}
	}
	for _, i := range a.Industries {
		if !industries[i] {
			return fmt.Errorf("unknown industry %q", i)
		}
	}
	return nil
}

// MaxProfileChars bounds a saved profile (it is sent with every scoring call).
const MaxProfileChars = 12000

// draftInstructions is the fixed system prompt for drafting a profile.
const draftInstructions = `You write a candidate profile that another model will use to score job postings for this candidate. Use only facts from the resume and the candidate's answers; never invent employers, dates, degrees, skills, or numbers. If something is unknown, leave it out.

Return GitHub-flavored markdown only (no code fences, no preamble), under 700 words, with exactly these sections:

# Candidate Profile
## Target roles
(the kinds of roles, seniority, and industries the candidate wants)
## Constraints and preferences
(work authorization and sponsorship needs, locations, remote/hybrid/onsite, things to avoid)
## Education
## Technical skills
(grouped, e.g. Languages, Data/ML, Cloud & infrastructure, Tools)
## Experience
(each role: title, employer, dates, then 2-4 short bullets with concrete accomplishments)
## Projects and research
(omit the section if there are none)
## Scoring guidance
- Highest fit: ...
- Good fit: ...
- Lower fit: ...
(derive these from the target roles, seniority, skills, and answers; mention roles requiring much more experience than the candidate has as lower fit)`

// DraftMessages builds the chat messages that draft a profile from the
// resume text, the questionnaire, and the chosen preferences.
func DraftMessages(resume string, a Answers, prefs config.Preferences, roles *config.Roles, industries []config.Industry) []scoring.Message {
	var b strings.Builder
	b.WriteString("<answers>\n")
	line := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			fmt.Fprintf(&b, "- %s: %s\n", label, scoring.Fence(v))
		}
	}
	line("Current or most recent title", a.CurrentTitle)
	if a.YearsExperience != nil {
		line("Years of professional experience", fmt.Sprintf("%g", *a.YearsExperience))
	}
	line("Work authorization", workAuthText[a.WorkAuthorization])
	line("Target roles (their words)", a.TargetRoles)
	line("Role families selected", labels(prefs.RoleFamilies, familyLabels(roles)))
	line("Seniority levels accepted", labels(prefs.Levels, levelLabels(roles)))
	if prefs.MaxYearsRequired > 0 {
		line("Skip roles requiring more than", fmt.Sprintf("%d years of experience", prefs.MaxYearsRequired))
	}
	line("Preferred locations", a.Locations)
	line("Work modes", strings.Join(a.WorkModes, ", "))
	line("Preferred industries", labels(a.Industries, industryLabels(industries)))
	line("Wants to avoid", a.Avoid)
	line("Other notes", a.Notes)

	b.WriteString("</answers>\n\n<resume>\n")
	if strings.TrimSpace(resume) == "" {
		b.WriteString("(no resume provided; rely on the answers)\n")
	} else {
		b.WriteString(scoring.Fence(resume))
	}
	b.WriteString("\n</resume>")
	return []scoring.Message{
		{Role: "system", Content: draftInstructions + "\n\n" + scoring.Untrusted},
		{Role: "user", Content: b.String()},
	}
}

// CleanDraft strips code fences and surrounding whitespace from a reply.
func CleanDraft(s string) string {
	s = strings.TrimSpace(scoring.StripFences(s))
	if len(s) > MaxProfileChars {
		s = s[:MaxProfileChars]
	}
	return s
}

func labels(ids []string, names map[string]string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := names[id]; n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, "; ")
}

func familyLabels(r *config.Roles) map[string]string {
	m := map[string]string{}
	for _, f := range r.RoleFamilies {
		m[f.ID] = f.Label
	}
	return m
}

func levelLabels(r *config.Roles) map[string]string {
	m := map[string]string{}
	for _, l := range r.Levels {
		m[l.ID] = l.Label
	}
	return m
}

func industryLabels(in []config.Industry) map[string]string {
	m := map[string]string{}
	for _, i := range in {
		m[i.ID] = i.Label
	}
	return m
}

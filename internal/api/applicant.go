package api

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wera/internal/profile"
	"wera/internal/resume"
	"wera/internal/store"
)

// applicantView is what application forms can be filled from: the
// details the user saved, completed from their profile answers and their
// resume, plus the jobs and schools on the resume.
type applicantView struct {
	Applicant *store.Applicant `json:"applicant"`
	Saved     bool             `json:"saved"`
	// Sources says where each filled-in (not saved) detail came from: "resume" or "profile".
	Sources    map[string]string   `json:"sources"`
	Experience []resume.Experience `json:"experience"`
	Education  []resume.Education  `json:"education"`
	Skills     []string            `json:"skills"`
}

// Placeholder rows of the starter resume, never real history.
var starterRows = map[string]bool{"university|degree": true, "company|job title": true}

var (
	resumeLinkedIn = regexp.MustCompile(`(?i)(?:https?://)?(?:[a-z]{2,3}\.)?linkedin\.com/in/[A-Za-z0-9_-]+/?`)
	resumeGitHub   = regexp.MustCompile(`(?i)(?:https?://)?github\.com/[A-Za-z0-9_-]+/?`)
	resumePhone    = regexp.MustCompile(`(?:\+?\d{1,2}[\s.-]?)?\(?\d{3}\)?[\s.-]?\d{3}[\s.-]?\d{4}`)
)

func withHTTPS(u string) string {
	u = strings.TrimSuffix(u, "/")
	if !strings.HasPrefix(strings.ToLower(u), "http") {
		return "https://" + u
	}
	return u
}

// applicantFor builds the view. Saved details always win; blanks are
// filled from the profile answers, then the main resume (or, without
// one, the links and phone number in the uploaded resume, while the main
// resume is built from it in the background).
func (s *Server) applicantFor(ctx context.Context, user *store.User) (*applicantView, error) {
	a, saved, err := store.GetApplicant(ctx, s.Pool, user)
	if err != nil {
		return nil, err
	}
	v := &applicantView{Applicant: a, Saved: saved, Sources: map[string]string{}, Experience: []resume.Experience{}, Education: []resume.Education{}, Skills: []string{}}
	fields := applicantFields(a)
	fill := func(key, value, source string) {
		p, ok := fields[key]
		if !ok || *p != "" || strings.TrimSpace(value) == "" || len(value) > 300 {
			return
		}
		*p = strings.TrimSpace(value)
		v.Sources[key] = source
	}

	prof, err := store.GetProfile(ctx, s.Pool, user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if prof != nil && len(prof.Answers) > 0 {
		var ans profile.Answers
		if json.Unmarshal(prof.Answers, &ans) == nil {
			fill("current_title", ans.CurrentTitle, "profile")
			if ans.YearsExperience != nil && *ans.YearsExperience > 0 {
				fill("years_experience", strconv.FormatFloat(*ans.YearsExperience, 'f', -1, 64), "profile")
			}
			switch ans.WorkAuthorization {
			case "citizen_or_resident":
				fill("authorized_to_work", "yes", "profile")
				fill("needs_sponsorship", "no", "profile")
			case "sponsorship_future":
				fill("authorized_to_work", "yes", "profile")
				fill("needs_sponsorship", "yes", "profile")
			case "sponsorship_now":
				fill("authorized_to_work", "no", "profile")
				fill("needs_sponsorship", "yes", "profile")
			}
			if len(ans.WorkModes) == 1 {
				fill("work_preference", ans.WorkModes[0], "profile")
			} else if len(ans.WorkModes) == 3 {
				fill("work_preference", "any", "profile")
			}
		}
	}

	base, err := store.BaseResume(ctx, s.Pool, user.ID)
	switch {
	case err == nil:
		b := resume.BackgroundOf(&base.Data, time.Now())
		for _, x := range b.Experience {
			if !starterRows[strings.ToLower(x.Company+"|"+x.Title)] {
				v.Experience = append(v.Experience, x)
			}
		}
		for _, e := range b.Education {
			if !starterRows[strings.ToLower(e.School+"|"+e.Degree)] {
				v.Education = append(v.Education, e)
			}
		}
		v.Skills = b.Skills
		// Details from the starter's placeholder rows are dropped with them.
		if len(v.Experience) == 0 {
			delete(b.Details, "current_company")
			delete(b.Details, "current_title")
			delete(b.Details, "years_experience")
		}
		if len(v.Education) == 0 {
			for _, k := range []string{"school", "degree", "major", "graduation_year", "gpa"} {
				delete(b.Details, k)
			}
		}
		for k, val := range b.Details {
			fill(k, val, "resume")
		}
	case errors.Is(err, pgx.ErrNoRows):
		if prof != nil && strings.TrimSpace(prof.ResumeText) != "" {
			if m := resumeLinkedIn.FindString(prof.ResumeText); m != "" {
				fill("linkedin", withHTTPS(m), "resume")
			}
			if m := resumeGitHub.FindString(prof.ResumeText); m != "" {
				fill("github", withHTTPS(m), "resume")
			}
			if m := resumePhone.FindString(prof.ResumeText); m != "" {
				fill("phone", m, "resume")
			}
			// The jobs and schools come once the main resume is built.
			s.importInBackground(user.ID, prof.ResumeText)
		}
	default:
		return nil, err
	}
	return v, nil
}

// applicantFields maps each detail's JSON name to its field.
func applicantFields(a *store.Applicant) map[string]*string {
	return map[string]*string{
		"first_name": &a.FirstName, "middle_name": &a.MiddleName, "last_name": &a.LastName, "email": &a.Email, "phone": &a.Phone,
		"city": &a.City, "state": &a.State, "country": &a.Country, "linkedin": &a.LinkedIn, "github": &a.GitHub, "portfolio": &a.Portfolio,
		"current_company": &a.CurrentCompany, "current_title": &a.CurrentTitle, "years_experience": &a.YearsExperience,
		"school": &a.School, "degree": &a.Degree, "major": &a.Major, "graduation_year": &a.GraduationYear, "gpa": &a.GPA,
		"authorized_to_work": &a.AuthorizedToWork, "needs_sponsorship": &a.NeedsSponsorship, "work_preference": &a.WorkPreference,
	}
}

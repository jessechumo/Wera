// Package filter implements the rule engine (PLAN.md section 7.3): a small,
// fast pre-LLM filter. Everything here is table-tested; false positives
// are safe (the LLM pass re-decides) but hard rules keep jobs OUT (and are
// visible in excluded jobs with readable evidence).
//
// The engine compiles the roles.yaml catalog once; each call applies one
// user's Preferences to one job.
package filter

import (
	"fmt"
	"regexp"
	"sort"

	"wera/internal/config"
)

// Stage names for jobs after rule filtering.
const (
	StageExcluded     = "excluded"
	StagePendingScore = "pending_score"
)

type patternGroup struct {
	name string
	res  []*regexp.Regexp
}

type override struct {
	patternGroup
	families []string
}

// Engine applies the roles.yaml rules to a job for one user's preferences.
type Engine struct {
	families    []patternGroup // catalog order
	overrides   []override     // sorted by name
	levels      []patternGroup // catalog order; only levels with patterns
	sponsorNo   []*regexp.Regexp
	sponsorFlag []*regexp.Regexp
	usPositive  []*regexp.Regexp
	nonUS       []*regexp.Regexp
}

// Result is the outcome of applying the rules to one job.
type Result struct {
	Stage      string
	Categories []string
	Reason     string // e.g. "title:senior"
	Evidence   string // matched sentence / title / location
	Flags      []string
}

// New compiles a Roles catalog into an Engine.
func New(r *config.Roles) (*Engine, error) {
	e := &Engine{}

	for _, f := range r.RoleFamilies {
		res, err := compileAll(f.Patterns)
		if err != nil {
			return nil, fmt.Errorf("role_families.%s: %w", f.ID, err)
		}
		e.families = append(e.families, patternGroup{name: f.ID, res: res})
	}
	for name, o := range r.TitleOverrides {
		res, err := compileAll(o.Patterns)
		if err != nil {
			return nil, fmt.Errorf("title_overrides.%s: %w", name, err)
		}
		e.overrides = append(e.overrides, override{patternGroup{name: name, res: res}, o.Families})
	}
	sort.Slice(e.overrides, func(i, j int) bool { return e.overrides[i].name < e.overrides[j].name })
	for _, l := range r.Levels {
		if len(l.Patterns) == 0 {
			continue
		}
		res, err := compileAll(l.Patterns)
		if err != nil {
			return nil, fmt.Errorf("levels.%s: %w", l.ID, err)
		}
		e.levels = append(e.levels, patternGroup{name: l.ID, res: res})
	}

	var err error
	if e.sponsorNo, err = compileAll(r.Sponsorship.ExcludePatterns); err != nil {
		return nil, fmt.Errorf("sponsorship.exclude_patterns: %w", err)
	}
	if e.sponsorFlag, err = compileAll(r.Sponsorship.FlagPatterns); err != nil {
		return nil, fmt.Errorf("sponsorship.flag_patterns: %w", err)
	}
	if e.usPositive, err = compileAll(usPositivePatterns); err != nil {
		return nil, fmt.Errorf("location rules: %w", err)
	}
	if e.nonUS, err = compileAll(nonUSPatterns); err != nil {
		return nil, fmt.Errorf("location rules: %w", err)
	}
	return e, nil
}

func compileAll(pats []string) ([]*regexp.Regexp, error) {
	if len(pats) == 0 {
		return nil, nil
	}
	res := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		re, err := regexp.Compile(`(?i)` + p)
		if err != nil {
			return nil, fmt.Errorf("bad pattern %q: %w", p, err)
		}
		res = append(res, re)
	}
	return res, nil
}

// Apply runs the rule pipeline on one job for one user. Steps (in order):
//
//  0. a title override for one of the user's families matches -> skip
//     the seniority and family checks
//  1. title shows a seniority level the user does not accept -> title:<tag>
//  2. no selected role family matches -> title:no_category
//  3. user wants US only and the location is clearly non-US -> location:non_us
//  4. user needs sponsorship and the posting refuses it ->
//     sponsorship:explicit_no (+ sentence)
//  5. else keep for scoring, record categories and flags
func (e *Engine) Apply(p *config.Preferences, title, location, description string) Result {
	result := Result{Categories: []string{}, Flags: []string{}}
	selected := make(map[string]bool, len(p.RoleFamilies))
	for _, f := range p.RoleFamilies {
		selected[f] = true
	}

	// Step 0: overrides.
	var cats []string
	for _, o := range e.overrides {
		if !anySelected(selected, o.families) || !matchAny(o.res, title) {
			continue
		}
		cats = append(cats, o.name)
		break
	}
	overridden := len(cats) > 0

	if !overridden {
		// Step 1: seniority levels the user does not accept.
		for _, l := range e.levels {
			if p.AcceptsLevel(l.name) {
				continue
			}
			for _, re := range l.res {
				if m := re.FindString(title); m != "" {
					return excluded(result, "title:"+Tag(m), title)
				}
			}
		}
		// Step 2: must match at least one selected role family.
		for _, f := range e.families {
			if selected[f.name] && matchAny(f.res, title) {
				cats = append(cats, f.name)
			}
		}
		if len(cats) == 0 {
			return excluded(result, "title:no_category", title)
		}
	}

	// Step 3: location.
	if p.USOnly && e.looksNonUS(location) {
		return excluded(result, "location:non_us", location)
	}

	// Step 4: explicit sponsorship refusals.
	if p.NeedsSponsorship {
		for _, re := range e.sponsorNo {
			if re.MatchString(description) {
				return excluded(result, "sponsorship:explicit_no", sentenceFor(re, description))
			}
		}
	}

	// Step 5: keep, collect all matched categories and ITAR-like flags.
	result.Stage = StagePendingScore
	result.Categories = cats
	for _, re := range e.sponsorFlag {
		if m := re.FindString(description); m != "" {
			result.Flags = append(result.Flags, Tag(m))
		}
	}
	return result
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func anySelected(selected map[string]bool, ids []string) bool {
	for _, id := range ids {
		if selected[id] {
			return true
		}
	}
	return false
}

func excluded(result Result, reason, evidence string) Result {
	result.Stage = StageExcluded
	result.Reason = reason
	result.Evidence = evidence
	return result
}

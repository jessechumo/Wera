// Package filter implements the rule engine (PLAN.md section 7.3): a small,
// fast pre-LLM filter. Everything here is table-tested; false positives
// are safe (the LLM pass re-decides) but hard rules keep jobs OUT (and are
// visible in excluded jobs with readable evidence).
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

// Engine applies the roles.yaml rules to a job.
type Engine struct {
	overrides   []patternGroup // order: sorted by name
	categories  []patternGroup
	exclude     []*regexp.Regexp
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

// New compiles a Rules config into an Engine.
func New(r *config.Roles) (*Engine, error) {
	e := &Engine{}

	for name, pats := range r.TitleOverrides {
		res, err := compileAll(pats)
		if err != nil {
			return nil, fmt.Errorf("title_overrides.%s: %w", name, err)
		}
		e.overrides = append(e.overrides, patternGroup{name: name, res: res})
	}
	for name, pats := range r.RoleCategories {
		res, err := compileAll(pats)
		if err != nil {
			return nil, fmt.Errorf("role_categories.%s: %w", name, err)
		}
		e.categories = append(e.categories, patternGroup{name: name, res: res})
	}
	sortGroups := func(g []patternGroup) {
		sort.Slice(g, func(i, j int) bool { return g[i].name < g[j].name })
	}
	sortGroups(e.overrides)
	sortGroups(e.categories)

	var err error
	if e.exclude, err = compileAll(r.ExcludeTitle); err != nil {
		return nil, fmt.Errorf("exclude_title: %w", err)
	}
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

// Apply runs the rule pipeline on one job. Steps (in order):
//
//  0. title_overrides match -> skip the exclude/no-category checks
//  1. exclude_title match -> title:<tag>
//  2. no role_categories match -> title:no_category
//  3. clearly non-US location -> location:non_us
//  4. sponsorship exclude pattern -> sponsorship:explicit_no (+ sentence)
//  5. else keep for scoring, record categories and flags
func (e *Engine) Apply(title, location, description string) Result {
	result := Result{Categories: []string{}, Flags: []string{}}

	// Step 0: overrides.
	var cats []string
	for _, o := range e.overrides {
		for _, re := range o.res {
			if re.MatchString(title) {
				cats = append(cats, o.name)
				break
			}
		}
		if len(cats) > 0 {
			break
		}
	}
	overridden := len(cats) > 0

	if !overridden {
		// Step 1: hard exclude on title.
		for _, re := range e.exclude {
			if m := re.FindString(title); m != "" {
				return excluded(result, "title:"+Tag(m), title)
			}
		}
		// Step 2: must match at least one role category.
		for _, c := range e.categories {
			for _, re := range c.res {
				if re.MatchString(title) {
					cats = append(cats, c.name)
					break
				}
			}
		}
		if len(cats) == 0 {
			return excluded(result, "title:no_category", title)
		}
	}

	// Step 3: location.
	if e.looksNonUS(location) {
		return excluded(result, "location:non_us", location)
	}

	// Step 4: explicit sponsorship refusals.
	for _, re := range e.sponsorNo {
		if re.MatchString(description) {
			return excluded(result, "sponsorship:explicit_no", sentenceFor(re, description))
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

func excluded(result Result, reason, evidence string) Result {
	result.Stage = StageExcluded
	result.Reason = reason
	result.Evidence = evidence
	return result
}

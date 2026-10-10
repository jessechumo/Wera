package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Roles is the parsed roles.yaml: the catalog of role families, seniority
// levels, and location/sponsorship rules. All matching logic lives here so
// that adding a role type is a YAML edit, never a code change. Each user's
// Preferences select from this catalog.
type Roles struct {
	RoleFamilies   []RoleFamily             `yaml:"role_families"`
	TitleOverrides map[string]TitleOverride `yaml:"title_overrides"`
	Levels         []Level                  `yaml:"levels"`
	Location       struct {
		AllowCountries []string `yaml:"allow_countries"`
	} `yaml:"location"`
	Sponsorship struct {
		ExcludePatterns []string `yaml:"exclude_patterns"`
		FlagPatterns    []string `yaml:"flag_patterns"`
	} `yaml:"sponsorship"`
}

// RoleFamily is one selectable kind of role (e.g. data_science).
type RoleFamily struct {
	ID          string   `yaml:"id" json:"id"`
	Label       string   `yaml:"label" json:"label"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Patterns    []string `yaml:"patterns" json:"-"`
}

// TitleOverride matches titles that would otherwise trip a seniority
// marker ("Member of Technical Staff"); it applies only to users who
// selected one of its families.
type TitleOverride struct {
	Families []string `yaml:"families"`
	Patterns []string `yaml:"patterns"`
}

// Level is one selectable seniority level. Patterns detect it from the
// title; a level without patterns (mid) is never detected.
type Level struct {
	ID       string   `yaml:"id" json:"id"`
	Label    string   `yaml:"label" json:"label"`
	Patterns []string `yaml:"patterns" json:"-"`
}

// FamilyIDs returns the set of valid role family ids.
func (r *Roles) FamilyIDs() map[string]bool {
	ids := make(map[string]bool, len(r.RoleFamilies))
	for _, f := range r.RoleFamilies {
		ids[f.ID] = true
	}
	return ids
}

// LevelIDs returns the set of valid level ids.
func (r *Roles) LevelIDs() map[string]bool {
	ids := make(map[string]bool, len(r.Levels))
	for _, l := range r.Levels {
		ids[l.ID] = true
	}
	return ids
}

// LoadRoles reads, parses and validates a roles.yaml file.
func LoadRoles(path string) (*Roles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var r Roles
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(r.Location.AllowCountries) == 0 {
		r.Location.AllowCountries = []string{"US"}
	}

	var errs []string
	fams := map[string]bool{}
	for i, f := range r.RoleFamilies {
		switch {
		case f.ID == "":
			errs = append(errs, fmt.Sprintf("role family %d: missing id", i+1))
		case fams[f.ID]:
			errs = append(errs, fmt.Sprintf("role family %q: duplicate id", f.ID))
		case len(f.Patterns) == 0:
			errs = append(errs, fmt.Sprintf("role family %q: no patterns", f.ID))
		}
		fams[f.ID] = true
	}
	levels := map[string]bool{}
	for i, l := range r.Levels {
		if l.ID == "" {
			errs = append(errs, fmt.Sprintf("level %d: missing id", i+1))
		} else if levels[l.ID] {
			errs = append(errs, fmt.Sprintf("level %q: duplicate id", l.ID))
		}
		levels[l.ID] = true
	}
	for name, o := range r.TitleOverrides {
		for _, f := range o.Families {
			if !fams[f] {
				errs = append(errs, fmt.Sprintf("title override %q: unknown family %q", name, f))
			}
		}
	}
	if len(r.RoleFamilies) == 0 || len(r.Levels) == 0 {
		errs = append(errs, "role_families and levels must not be empty")
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s validation failed:\n  - %s", path, strings.Join(errs, "\n  - "))
	}
	return &r, nil
}

// Preferences are one user's matching choices from the signup form. They
// drive the rule filter and the post-LLM exclusions for that user.
type Preferences struct {
	RoleFamilies     []string `json:"role_families"`
	Levels           []string `json:"levels"`
	MaxYearsRequired int      `json:"max_years_required"` // jobs asking for more are dropped after scoring
	USOnly           bool     `json:"us_only"`            // drop clearly non-US locations
	NeedsSponsorship bool     `json:"needs_sponsorship"`  // drop explicit sponsorship refusals
}

// Validate checks preferences against the catalog.
func (p *Preferences) Validate(r *Roles) error {
	var errs []string
	if len(p.RoleFamilies) == 0 {
		errs = append(errs, "pick at least one role family")
	}
	fams := r.FamilyIDs()
	for _, f := range p.RoleFamilies {
		if !fams[f] {
			errs = append(errs, fmt.Sprintf("unknown role family %q", f))
		}
	}
	if len(p.Levels) == 0 {
		errs = append(errs, "pick at least one seniority level")
	}
	levels := r.LevelIDs()
	for _, l := range p.Levels {
		if !levels[l] {
			errs = append(errs, fmt.Sprintf("unknown level %q", l))
		}
	}
	if p.MaxYearsRequired < 0 || p.MaxYearsRequired > 50 {
		errs = append(errs, "max_years_required must be between 0 and 50")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// AcceptsLevel reports whether the user accepts a seniority level id.
func (p *Preferences) AcceptsLevel(id string) bool {
	for _, l := range p.Levels {
		if l == id {
			return true
		}
	}
	return false
}

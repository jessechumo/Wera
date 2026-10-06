package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Roles is the parsed roles.yaml: all matching logic lives here so that
// adding a role type is a YAML edit, never a code change.
type Roles struct {
	TitleOverrides map[string][]string `yaml:"title_overrides"`
	RoleCategories map[string][]string `yaml:"role_categories"`
	ExcludeTitle   []string            `yaml:"exclude_title"`
	Seniority      struct {
		// MaxYearsRequired is the LLM-extracted years ceiling; jobs
		// requiring more are excluded after scoring.
		MaxYearsRequired int `yaml:"max_years_required"`
	} `yaml:"seniority"`
	Location struct {
		AllowCountries []string `yaml:"allow_countries"`
		AllowRemoteUS  bool     `yaml:"allow_remote_us"`
	} `yaml:"location"`
	Sponsorship struct {
		ExcludePatterns []string `yaml:"exclude_patterns"`
		FlagPatterns    []string `yaml:"flag_patterns"`
	} `yaml:"sponsorship"`
}

// LoadRoles reads and parses a roles.yaml file, applying defaults.
func LoadRoles(path string) (*Roles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var r Roles
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if r.Seniority.MaxYearsRequired == 0 {
		r.Seniority.MaxYearsRequired = 3
	}
	if len(r.Location.AllowCountries) == 0 {
		r.Location.AllowCountries = []string{"US"}
	}
	return &r, nil
}

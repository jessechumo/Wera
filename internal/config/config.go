// Package config loads and validates Wera configuration: the YAML files
// (companies, roles) and environment variables.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// KnownATS lists the supported applicant tracking systems.
var KnownATS = map[string]bool{
	"greenhouse": true,
	"lever":      true,
	"ashby":      true,
}

// KnownGroups lists the valid company groups.
var KnownGroups = map[string]bool{
	"ai_infra": true,
	"trading":  true,
	"other":    true,
}

// Default config paths, relative to the working directory.
const (
	DefaultCompaniesPath = "config/companies.yaml"
	DefaultRolesPath     = "config/roles.yaml"
	DefaultProfilePath   = "profile/profile.md"
)

// Company is one entry from config/companies.yaml.
type Company struct {
	Name    string `yaml:"name"`
	ATS     string `yaml:"ats"`
	Token   string `yaml:"token"`
	Group   string `yaml:"group"`
	Enabled *bool  `yaml:"enabled"`
	Notes   string `yaml:"notes"`
}

// IsEnabled reports whether the company should be fetched. A missing
// enabled key defaults to true.
func (c Company) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// Companies is the parsed companies.yaml.
type Companies struct {
	Companies []Company `yaml:"companies"`
}

// LoadCompanies reads and validates a companies.yaml file.
func LoadCompanies(path string) (*Companies, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Companies
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the company list for duplicates and invalid values,
// returning one combined error describing every problem found.
func (c *Companies) Validate() error {
	var errs []string
	seenName := map[string]bool{}
	seenBoard := map[string]bool{}
	for i, comp := range c.Companies {
		where := fmt.Sprintf("company %d", i+1)
		if comp.Name == "" {
			errs = append(errs, where+": missing name")
			continue
		}
		where = fmt.Sprintf("company %q", comp.Name)
		if seenName[comp.Name] {
			errs = append(errs, where+": duplicate name")
		}
		seenName[comp.Name] = true
		if !KnownATS[comp.ATS] {
			errs = append(errs, fmt.Sprintf("%s: invalid ats %q (want greenhouse, lever or ashby)", where, comp.ATS))
		}
		if comp.Token == "" {
			errs = append(errs, where+": missing token")
		}
		if !KnownGroups[comp.Group] {
			errs = append(errs, fmt.Sprintf("%s: invalid group %q (want ai_infra, trading or other)", where, comp.Group))
		}
		if comp.Token != "" {
			board := comp.ATS + "/" + comp.Token
			if seenBoard[board] {
				errs = append(errs, fmt.Sprintf("%s: duplicate board %s", where, board))
			}
			seenBoard[board] = true
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("companies.yaml validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

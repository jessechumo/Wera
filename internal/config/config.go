// Package config loads and validates Wera configuration: the YAML files
// (companies, roles) and environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// KnownATS lists the supported applicant tracking systems.
var KnownATS = map[string]bool{
	"greenhouse":      true,
	"lever":           true,
	"ashby":           true,
	"workday":         true,
	"smartrecruiters": true,
	"eightfold":       true,
	"amazon":          true,
}

// Default config paths, relative to the working directory.
const (
	DefaultCompaniesPath  = "config/companies.yaml"
	DefaultIndustriesPath = "config/industries.yaml"
	DefaultRolesPath      = "config/roles.yaml"
	DefaultProfilePath    = "profile/profile.md"
)

// Company is one entry from config/companies.yaml.
type Company struct {
	Name     string `yaml:"name"`
	ATS      string `yaml:"ats"`
	Token    string `yaml:"token"`
	Industry string `yaml:"industry"`
	Enabled  *bool  `yaml:"enabled"`
	Notes    string `yaml:"notes"`
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

// LoadCompanies reads and validates a companies.yaml file. Industry ids
// are checked against the industries.yaml in the same directory.
func LoadCompanies(path string) (*Companies, error) {
	inds, err := LoadIndustries(filepath.Join(filepath.Dir(path), "industries.yaml"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // config file path from the operator
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Companies
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(inds.IDs()); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the company list for duplicates and invalid values
// (industry must be one of industries), returning one combined error
// describing every problem found.
func (c *Companies) Validate(industries map[string]bool) error {
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
			errs = append(errs, fmt.Sprintf("%s: invalid ats %q (want one of %s)", where, comp.ATS, atsNames()))
		}
		if comp.Token == "" {
			errs = append(errs, where+": missing token")
		}
		if !industries[comp.Industry] {
			errs = append(errs, fmt.Sprintf("%s: invalid industry %q (see industries.yaml)", where, comp.Industry))
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

// Industry is one entry from config/industries.yaml.
type Industry struct {
	ID          string `yaml:"id" json:"id"`
	Label       string `yaml:"label" json:"label"`
	Description string `yaml:"description" json:"description"`
}

// Industries is the parsed industries.yaml, in display order.
type Industries struct {
	Industries []Industry `yaml:"industries"`
}

// IDs returns the set of valid industry ids.
func (in *Industries) IDs() map[string]bool {
	ids := make(map[string]bool, len(in.Industries))
	for _, i := range in.Industries {
		ids[i.ID] = true
	}
	return ids
}

// LoadIndustries reads and validates an industries.yaml file.
func LoadIndustries(path string) (*Industries, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config file path from the operator
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var in Industries
	if err := yaml.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var errs []string
	seen := map[string]bool{}
	for i, ind := range in.Industries {
		switch {
		case ind.ID == "":
			errs = append(errs, fmt.Sprintf("industry %d: missing id", i+1))
		case seen[ind.ID]:
			errs = append(errs, fmt.Sprintf("industry %q: duplicate id", ind.ID))
		case ind.Label == "":
			errs = append(errs, fmt.Sprintf("industry %q: missing label", ind.ID))
		}
		seen[ind.ID] = true
	}
	if len(in.Industries) == 0 {
		errs = append(errs, "no industries defined")
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("industries.yaml validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return &in, nil
}

// atsNames lists the known ATS names, sorted, for error messages.
func atsNames() string {
	names := make([]string, 0, len(KnownATS))
	for n := range KnownATS {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

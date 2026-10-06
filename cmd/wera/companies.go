// Command dispatch for `wera companies`: YAML validation and summaries.
package main

import (
	"context"
	"fmt"

	"wera/internal/config"
)

// runCompanies implements `wera companies validate`: it loads
// companies.yaml (validation errors list every duplicate name/board and
// every bad ats/group value) and prints a summary.
func runCompanies(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "validate" {
		return fmt.Errorf("usage: wera companies validate")
	}
	comps, err := config.LoadCompanies(config.DefaultCompaniesPath)
	if err != nil {
		return err
	}

	enabled, disabled := 0, 0
	perATS := map[string]int{"greenhouse": 0, "lever": 0, "ashby": 0}
	for _, c := range comps.Companies {
		perATS[c.ATS]++
		if c.IsEnabled() {
			enabled++
		} else {
			disabled++
		}
	}

	fmt.Printf("companies.yaml OK: %d companies (%d enabled, %d disabled)\n",
		len(comps.Companies), enabled, disabled)
	for _, ats := range []string{"greenhouse", "lever", "ashby"} {
		fmt.Printf("  %-10s %d\n", ats, perATS[ats])
	}
	return nil
}

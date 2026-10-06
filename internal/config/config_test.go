package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "companies.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCompaniesValid(t *testing.T) {
	path := writeTemp(t, `
companies:
  - { name: DRW, ats: greenhouse, token: drweng, group: trading, enabled: true }
  - { name: Baseten, ats: ashby, token: baseten, group: ai_infra, enabled: false, notes: "ok" }
`)
	c, err := LoadCompanies(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Companies) != 2 {
		t.Fatalf("want 2 companies, got %d", len(c.Companies))
	}
	if !c.Companies[0].IsEnabled() {
		t.Error("first company should be enabled")
	}
	if c.Companies[1].IsEnabled() {
		t.Error("second company should be disabled")
	}
}

func TestLoadCompaniesDefaultsEnabledTrue(t *testing.T) {
	path := writeTemp(t, `
companies:
  - { name: X, ats: lever, token: xtoken, group: other }
`)
	c, err := LoadCompanies(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.Companies[0].IsEnabled() {
		t.Error("missing enabled key should default to enabled")
	}
}

func TestLoadCompaniesErrors(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantSub []string
	}{
		{"duplicate name", `
companies:
  - { name: A, ats: greenhouse, token: a1, group: trading, enabled: true }
  - { name: A, ats: lever, token: a2, group: trading, enabled: true }
`, []string{"duplicate name"}},
		{"duplicate board", `
companies:
  - { name: A, ats: greenhouse, token: same, group: trading, enabled: true }
  - { name: B, ats: greenhouse, token: same, group: trading, enabled: true }
`, []string{"duplicate board"}},
		{"bad ats", `
companies:
  - { name: A, ats: workday, token: a1, group: trading, enabled: true }
`, []string{"invalid ats"}},
		{"bad group", `
companies:
  - { name: A, ats: greenhouse, token: a1, group: finance, enabled: true }
`, []string{"invalid group"}},
		{"missing token", `
companies:
  - { name: A, ats: greenhouse, group: trading, enabled: true }
`, []string{"missing token"}},
		{"missing name", `
companies:
  - { ats: greenhouse, token: a1, group: trading, enabled: true }
`, []string{"missing name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadCompanies(writeTemp(t, tc.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not mention %q", err, sub)
				}
			}
		})
	}
}

func TestLoadRoles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roles.yaml")
	content, err := os.ReadFile("../../config/roles.yaml")
	if err != nil {
		t.Skip("roles.yaml not available")
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRoles(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.TitleOverrides) == 0 || len(r.RoleCategories) == 0 {
		t.Error("expected overrides and categories to parse")
	}
	if len(r.ExcludeTitle) == 0 || len(r.Sponsorship.ExcludePatterns) == 0 {
		t.Error("expected exclude patterns to parse")
	}
	if r.Seniority.MaxYearsRequired != 3 {
		t.Errorf("max_years_required: want 3, got %d", r.Seniority.MaxYearsRequired)
	}
}

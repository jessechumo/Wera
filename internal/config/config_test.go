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
	inds := "industries:\n  - { id: trading, label: Trading }\n  - { id: ai_ml, label: AI }\n"
	if err := os.WriteFile(filepath.Join(dir, "industries.yaml"), []byte(inds), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCompaniesValid(t *testing.T) {
	path := writeTemp(t, `
companies:
  - { name: DRW, ats: greenhouse, token: drweng, industry: trading, enabled: true }
  - { name: Baseten, ats: ashby, token: baseten, industry: ai_ml, enabled: false, notes: "ok" }
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
  - { name: X, ats: lever, token: xtoken, industry: trading }
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
  - { name: A, ats: greenhouse, token: a1, industry: trading, enabled: true }
  - { name: A, ats: lever, token: a2, industry: trading, enabled: true }
`, []string{"duplicate name"}},
		{"duplicate board", `
companies:
  - { name: A, ats: greenhouse, token: same, industry: trading, enabled: true }
  - { name: B, ats: greenhouse, token: same, industry: trading, enabled: true }
`, []string{"duplicate board"}},
		{"bad ats", `
companies:
  - { name: A, ats: workday, token: a1, industry: trading, enabled: true }
`, []string{"invalid ats"}},
		{"bad industry", `
companies:
  - { name: A, ats: greenhouse, token: a1, industry: finance, enabled: true }
`, []string{"invalid industry"}},
		{"missing token", `
companies:
  - { name: A, ats: greenhouse, industry: trading, enabled: true }
`, []string{"missing token"}},
		{"missing name", `
companies:
  - { ats: greenhouse, token: a1, industry: trading, enabled: true }
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
	if len(r.RoleFamilies) == 0 || len(r.Levels) == 0 || len(r.TitleOverrides) == 0 {
		t.Error("expected families, levels and overrides to parse")
	}
	if len(r.Sponsorship.ExcludePatterns) == 0 {
		t.Error("expected sponsorship patterns to parse")
	}
	good := Preferences{RoleFamilies: []string{"data_science"}, Levels: []string{"entry"}, MaxYearsRequired: 3}
	if err := good.Validate(r); err != nil {
		t.Errorf("valid preferences rejected: %v", err)
	}
	bad := Preferences{RoleFamilies: []string{"astrology"}, Levels: nil}
	if err := bad.Validate(r); err == nil {
		t.Error("invalid preferences accepted")
	}
}

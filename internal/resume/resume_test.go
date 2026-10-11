package resume

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func sample(t *testing.T) *Resume {
	t.Helper()
	src, err := os.ReadFile("testdata/sample.tex")
	if err != nil {
		t.Fatal(err)
	}
	r, warnings, err := ImportTeX(string(src))
	if err != nil || len(warnings) > 0 {
		t.Fatalf("import: %v %v", err, warnings)
	}
	return r
}

func TestImportTeX(t *testing.T) {
	r := sample(t)
	if r.Name != "Ada Lovelace" || r.Phone != "+1 555 010 0199" || r.Email != "ada@example.com" || len(r.Links) != 2 ||
		r.Links[1].Label != "github.com/ada-example" || r.Links[1].URL != "https://github.com/ada-example" {
		t.Fatalf("header: %+v", r)
	}
	kinds := []Kind{}
	for _, s := range r.Sections {
		kinds = append(kinds, s.Kind)
	}
	if strings.Join(toStrings(kinds), ",") != "entries,skills,entries,projects,compact" {
		t.Fatalf("sections: %v", kinds)
	}
	exp := r.Sections[2].Entries
	if exp[0].Heading != "Analytical Engines Inc." || exp[0].Subheading != "Site Reliability Engineer" || exp[0].Dates != "Jan 2025 – Present" ||
		len(exp[0].Bullets) != 2 || exp[0].Bullets[0].Text != "Cut deploy time by 50% with GitHub Actions pipelines across 12 services." {
		t.Errorf("experience: %+v", exp[0])
	}
	if exp[1].Bullets[0].Text != "Built Python tooling for automated provisioning." {
		t.Errorf("formatting not stripped: %q", exp[1].Bullets[0].Text)
	}
	skills := r.Sections[1].Skills
	if len(skills) != 2 || skills[1].Name != "Infrastructure & Cloud" || !strings.HasPrefix(skills[1].Items, "Kubernetes, Docker") {
		t.Errorf("skills: %+v", skills)
	}
	proj := r.Sections[3].Entries[0]
	if proj.Text != "A Go analyzer for numerical programs." || proj.LinkLabel != "Published thesis" || proj.URL != "https://example.com/thesis" {
		t.Errorf("project: %+v", proj)
	}
	if c := r.Sections[4].Entries[0]; c.Heading != "Teaching Assistant, Algorithms" || c.Subheading != "Example State University" {
		t.Errorf("compact: %+v", c)
	}
}

func toStrings(ks []Kind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

func TestExportTeXRoundTripAndEscaping(t *testing.T) {
	r := sample(t)
	back, _, err := ImportTeX(r.ExportTeX())
	if err != nil {
		t.Fatal(err)
	}
	if back.PlainText() != r.PlainText() {
		t.Errorf("round trip changed the resume:\n%s\n---\n%s", r.PlainText(), back.PlainText())
	}
	r.Sections[2].Entries[0].Bullets[0].Text = `Saved $5 & 10% \input{/etc/passwd} }{\write18{rm -rf /}} #1 _x_ ^~`
	tex := r.ExportTeX()
	for _, bad := range []string{`\input{/etc/passwd}`, `\write18{`} {
		if strings.Contains(tex, bad) {
			t.Errorf("unescaped %q in exported LaTeX", bad)
		}
	}
	if !strings.Contains(tex, `Saved \$5 \& 10\% \textbackslash{}input\{/etc/passwd\}`) {
		t.Errorf("escaping wrong: %s", tex[strings.Index(tex, "Saved"):][:120])
	}
}

func TestNormalize(t *testing.T) {
	r := &Resume{Name: " Ada ", Links: []Link{{URL: "https://x.dev/a"}}, Sections: []Section{{Kind: KindEntries, Title: "Exp",
		Entries: []Entry{{Heading: "A", Bullets: []Bullet{{Text: " keep "}, {Text: "  "}}}}}}}
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	e := r.Sections[0].Entries[0]
	if r.Name != "Ada" || r.Sections[0].ID == "" || e.ID == "" || len(e.Bullets) != 1 || e.Bullets[0].ID == "" || r.Links[0].Label != "x.dev/a" {
		t.Errorf("normalize: %+v", r)
	}
	for name, bad := range map[string]*Resume{
		"no name":        {Sections: []Section{}},
		"javascript url": {Name: "A", Links: []Link{{URL: "javascript:alert(1)"}}},
		"entry url":      {Name: "A", Sections: []Section{{Kind: KindProjects, Entries: []Entry{{URL: "data:text/html,x"}}}}},
		"kind":           {Name: "A", Sections: []Section{{Kind: "html"}}},
		"long bullet":    {Name: "A", Sections: []Section{{Kind: KindEntries, Entries: []Entry{{Bullets: []Bullet{{Text: strings.Repeat("x", 700)}}}}}}},
	} {
		if err := bad.Normalize(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestKeywordCoverage(t *testing.T) {
	text := sample(t).PlainText()
	cov := KeywordCoverage(text, []string{"k8s", "Golang", "Terraform", "Postgres", "CI/CD", "GitHub Actions", "Rust", "golang"})
	if strings.Join(cov.Matched, ",") != "k8s,Golang,Terraform,CI/CD,GitHub Actions" || strings.Join(cov.Missing, ",") != "Postgres,Rust" || cov.Percent != 71 {
		t.Errorf("%+v", cov)
	}
	alt := KeywordCoverage("Built pipelines with Airflow and Python/Go services", []string{
		"ML lifecycle tooling (Kubeflow/Airflow/MLflow)", "gradient-boosted trees (LightGBM/XGBoost)", "Go/Rust", "CI/CD"})
	if strings.Join(alt.Matched, ",") != "ML lifecycle tooling (Kubeflow/Airflow/MLflow),Go/Rust" {
		t.Errorf("alternatives: %+v", alt)
	}
	if got := KnownTerms("We use Kubernetes, Go and PostgreSQL; experience with k8s and Terraform."); strings.Join(got, ",") != "kubernetes,go,postgresql,terraform" {
		t.Errorf("KnownTerms: %v", got)
	}
}

// --- rendering (needs the typst binary) -------------------------------------

func renderer(t *testing.T) *Renderer {
	t.Helper()
	rd := NewRenderer(os.Getenv("TYPST_BIN"))
	if !rd.Available() {
		if os.Getenv("CI") != "" {
			t.Fatal("typst must be installed in CI")
		}
		t.Skip("typst not installed")
	}
	return rd
}

func TestRenderPDFAndSVG(t *testing.T) {
	rd := renderer(t)
	r := sample(t)
	// Text that looks like Typst code must print as text.
	r.Sections[2].Entries[0].Bullets[1].Text = `#eval("1+1") ]#panic("x") $x^2$ @ref <label> // not a comment`
	ctx := context.Background()
	pdf, err := rd.PDF(ctx, r, DefaultLayout)
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("pdf: %v", err)
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		cmd := exec.Command("pdftotext", "-", "-")
		cmd.Stdin = strings.NewReader(string(pdf))
		out, _ := cmd.Output()
		txt := strings.Join(strings.Fields(string(out)), " ")
		for _, want := range []string{"ADA LOVELACE", `#eval("1+1") ]#panic("x") $x^2$ @ref <label> // not a comment`, "Published thesis"} {
			if !strings.Contains(strings.ToUpper(txt), strings.ToUpper(want)) {
				t.Errorf("PDF text is missing %q:\n%s", want, txt)
			}
		}
	}
	pages, err := rd.SVG(ctx, r, DefaultLayout)
	if err != nil || len(pages) != 1 || !strings.Contains(string(pages[0]), "<svg") || strings.Contains(string(pages[0]), "<script") {
		t.Fatalf("svg: %d pages, %v", len(pages), err)
	}
	m, err := rd.Measure(ctx, r, DefaultLayout)
	if err != nil || m.Pages != 1 || m.Fill <= 0 || m.Fill >= 1 {
		t.Errorf("measure: %+v %v", m, err)
	}
}

func TestFitToOnePage(t *testing.T) {
	rd := renderer(t)
	ctx := context.Background()

	short := sample(t)
	res, err := rd.Fit(ctx, short, nil)
	if err != nil || !res.OnePage || res.Layout != ladder[0] || len(res.Hidden) != 0 {
		t.Fatalf("a short resume should fit as written: %+v %v", res, err)
	}

	// A bit long: tighter type and spacing are enough, nothing is hidden.
	medium := withRoles(t, 4, 5)
	res, err = rd.Fit(ctx, medium, nil)
	if err != nil || !res.OnePage || len(res.Hidden) != 0 || res.Layout == ladder[0] {
		t.Fatalf("medium: want a tighter layout and nothing hidden: %+v %v", res, err)
	}

	// Much too long: bullets must go.
	long := withRoles(t, 7, 7)
	exp := &long.Sections[2]
	if m, _ := rd.Measure(ctx, long, ladder[0]); m.Pages < 2 {
		t.Fatalf("test resume should start on 2+ pages, got %+v", m)
	}
	res, err = rd.Fit(ctx, long, nil)
	if err != nil || !res.OnePage || res.After.Pages != 1 || len(res.Hidden) == 0 {
		t.Fatalf("fit: %+v %v", res, err)
	}
	for _, s := range long.Sections {
		for _, e := range s.Entries {
			if len(e.Bullets) > 0 && e.Bullets[0].Hidden {
				t.Errorf("a role's first bullet was hidden: %s", e.Heading)
			}
		}
	}
	// The newest role keeps its bullets; older ones give theirs up first.
	if exp.Entries[0].Bullets[1].Hidden {
		t.Error("the most recent role lost a bullet before older ones")
	}
	if m, _ := rd.Measure(ctx, long, res.Layout); m.Pages != 1 {
		t.Errorf("the fitted resume is %d pages", m.Pages)
	}
	t.Logf("fit: %s (%d measurements)", res.Summary, res.Attempts)
}

// withRoles is the sample plus extra experience entries.
func withRoles(t *testing.T, roles, bullets int) *Resume {
	t.Helper()
	r := sample(t)
	exp := &r.Sections[2]
	for i := 0; i < roles; i++ {
		e := Entry{Heading: "Company " + string(rune('A'+i)), Subheading: "Engineer", Dates: "2020", Location: "Remote"}
		for j := 0; j < bullets; j++ {
			e.Bullets = append(e.Bullets, Bullet{Text: "Delivered a substantial improvement to a production system used by many people every single day, measured carefully."})
		}
		exp.Entries = append(exp.Entries, e)
	}
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	return r
}

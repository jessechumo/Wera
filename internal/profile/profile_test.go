package profile

import (
	"bytes"
	"image"
	pngenc "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wera/internal/config"
)

func TestExtractPDFText(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sample-resume.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	extractors := map[string]func([]byte) (string, error){"go": goPDFText}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		extractors["poppler"] = ExtractPDFText
	}
	for name, extract := range extractors {
		text, err := extract(data)
		if err != nil {
			t.Fatalf("%s extract: %v", name, err)
		}
		for _, want := range []string{"Alex Example\nData Scientist | Dallas, TX",
			"Data Scientist, Example Airlines (2022 - present)\n- Built a delay-prediction model", "PyTorch"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: extracted text is missing %q:\n%s", name, want, text)
			}
		}
	}
	if _, err := ExtractPDFText([]byte("hello, not a pdf")); err != ErrNotPDF {
		t.Errorf("non-PDF: want ErrNotPDF, got %v", err)
	}
	if _, err := ExtractPDFText([]byte("%PDF-1.7 garbage")); err == nil {
		t.Error("garbage PDF: expected an error")
	}
}

func TestCleanText(t *testing.T) {
	got := CleanText("  Name \r\n\r\n\r\n\r\nSkills:\t Go,   SQL  \n")
	if got != "Name\n\nSkills: Go, SQL" {
		t.Errorf("CleanText = %q", got)
	}
}

func TestDraftMessages(t *testing.T) {
	roles, err := config.LoadRoles(filepath.Join("..", "..", "config", "roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	years := 3.0
	a := Answers{CurrentTitle: "Data Scientist", YearsExperience: &years,
		WorkAuthorization: "sponsorship_future", Industries: []string{"aerospace"},
		WorkModes: []string{"remote", "hybrid"}}
	prefs := config.Preferences{RoleFamilies: []string{"data_science", "ml_engineering"},
		Levels: []string{"entry", "mid"}, MaxYearsRequired: 5}
	inds := []config.Industry{{ID: "aerospace", Label: "Aviation & Aerospace"}}
	msgs := DraftMessages("RESUME TEXT", a, prefs, roles, inds)
	if len(msgs) != 2 || msgs[0].Role != "system" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	user := msgs[1].Content
	for _, want := range []string{"Data Scientist", "Years of professional experience: 3",
		"OPT", "Data science", "Aviation & Aerospace", "remote, hybrid", "more than: 5 years", "RESUME TEXT"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt is missing %q:\n%s", want, user)
		}
	}
	if err := a.Validate(map[string]bool{"aerospace": true}); err != nil {
		t.Errorf("valid answers rejected: %v", err)
	}
	bad := Answers{WorkAuthorization: "martian"}
	if err := bad.Validate(nil); err == nil {
		t.Error("bad work authorization accepted")
	}
	if got := CleanDraft("```markdown\n# Candidate Profile\n```"); got != "# Candidate Profile" {
		t.Errorf("CleanDraft = %q", got)
	}
}

func TestParseSuggestions(t *testing.T) {
	roles, err := config.LoadRoles(filepath.Join("..", "..", "config", "roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	reply := "```json\n" + `{"role_families":["data_science","astrology","ml_engineering","data_science"],` +
		`"levels":["entry","mid","senior","management"],"years_experience":3,"current_title":" Data Scientist ",` +
		`"locations":["Dallas, TX","Fort Worth, TX","Austin, TX","Houston, TX"],"target_roles":"Data Scientist, ML Engineer"}` + "\n```"
	s, err := ParseSuggestions(reply, roles)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.RoleFamilies, ",") != "data_science,ml_engineering" {
		t.Errorf("families: %v", s.RoleFamilies)
	}
	if len(s.Levels) != 3 || s.CurrentTitle != "Data Scientist" || len(s.Locations) != 3 ||
		s.YearsExperience == nil || *s.YearsExperience != 3 {
		t.Errorf("suggestions: %+v", s)
	}
	if _, err := ParseSuggestions("not json", roles); err == nil {
		t.Error("garbage accepted")
	}
	msgs := SuggestMessages("RESUME", roles)
	if !strings.Contains(msgs[1].Content, "- data_science: Data science") || !strings.Contains(msgs[1].Content, "RESUME") {
		t.Errorf("prompt: %s", msgs[1].Content)
	}
}

func TestNormalizeAvatar(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 300, 120)) // wide, transparent
	var png bytes.Buffer
	if err := pngenc.Encode(&png, src); err != nil {
		t.Fatal(err)
	}
	out, err := NormalizeAvatar(png.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "jpeg" || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("got %s %v %v", format, img.Bounds(), err)
	}
	if _, err := NormalizeAvatar([]byte("<svg onload=alert(1)>")); err != ErrBadImage {
		t.Errorf("non-image accepted: %v", err)
	}
}

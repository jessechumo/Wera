package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"wera/internal/resume"
)

func needTypst(t *testing.T) {
	t.Helper()
	if !resume.NewRenderer(os.Getenv("TYPST_BIN")).Available() {
		if os.Getenv("CI") != "" {
			t.Fatal("typst must be installed in CI")
		}
		t.Skip("typst not installed")
	}
}

func importSample(t *testing.T, base string, replace bool) (int, map[string]any) {
	t.Helper()
	tex, err := os.ReadFile("../resume/testdata/sample.tex")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"source": "tex", "tex": string(tex), "replace": replace})
	code, body := do(t, client, http.MethodPost, base+"/api/resumes/import", string(b))
	var out map[string]any
	json.Unmarshal([]byte(body), &out)
	return code, out
}

func TestResumeLifecycle(t *testing.T) {
	needTypst(t)
	ts, _ := testServer(t, nil)
	code, doc := importSample(t, ts.URL, false)
	if code != 201 || doc["data"].(map[string]any)["name"] != "Ada Lovelace" || doc["fit"].(map[string]any)["one_page"] != true {
		t.Fatalf("import: %d %v", code, doc)
	}
	id := int64(doc["id"].(float64))
	if code, _ := importSample(t, ts.URL, false); code != 409 {
		t.Errorf("second base resume: want 409, got %d", code)
	}
	if code, again := importSample(t, ts.URL, true); code != 201 || int64(again["id"].(float64)) != id {
		t.Errorf("replace keeps the same resume: %d", code)
	}

	url := fmt.Sprintf("%s/api/resumes/%d", ts.URL, id)
	data := doc["data"].(map[string]any)
	data["name"] = "Ada King"
	b, _ := json.Marshal(map[string]any{"data": data, "layout": map[string]any{"font_size": 9, "spacing": 1, "margin": 0.5}})
	code, body := do(t, client, http.MethodPut, url, string(b))
	if code != 200 || !strings.Contains(body, `"name":"Ada King"`) || !strings.Contains(body, `"font_size":10`) || !strings.Contains(body, `"fit":null`) {
		t.Errorf("save (font clamped to 10pt, fit cleared): %d %.300s", code, body)
	}
	data["links"] = []map[string]string{{"label": "x", "url": "javascript:alert(1)"}}
	b, _ = json.Marshal(map[string]any{"data": data})
	if code, _ := do(t, client, http.MethodPut, url, string(b)); code != 400 {
		t.Errorf("unsafe link: want 400, got %d", code)
	}

	data["links"] = []map[string]string{{"label": "github.com/ada", "url": "https://github.com/ada"}}

	resp, err := client.Get(url + "/pdf?download=1")
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.HasPrefix(string(pdf), "%PDF") || resp.Header.Get("Content-Disposition") != `attachment; filename="Ada_King_Resume.pdf"` {
		t.Errorf("pdf: %q %s", resp.Header.Get("Content-Disposition"), pdf[:min(10, len(pdf))])
	}
	if code, tex := do(t, client, http.MethodGet, url+"/tex", ""); code != 200 || !strings.Contains(tex, `\resumeSubheading`) || !strings.Contains(tex, "Ada King") {
		t.Errorf("tex: %d", code)
	}

	b, _ = json.Marshal(map[string]any{"data": doc["data"], "layout": map[string]any{}})
	code, body = do(t, client, http.MethodPost, ts.URL+"/api/resumes/preview", string(b))
	var prev struct {
		Pages   []string
		Measure struct{ Pages int }
	}
	json.Unmarshal([]byte(body), &prev)
	if code != 200 || len(prev.Pages) != 1 || !strings.HasPrefix(prev.Pages[0], "<svg") || prev.Measure.Pages != 1 {
		t.Errorf("preview: %d %.200s", code, body)
	}
	if code, body := do(t, client, http.MethodPost, url+"/fit", ""); code != 200 || !strings.Contains(body, `"one_page":true`) {
		t.Errorf("fit: %d %.200s", code, body)
	}
	if _, body := do(t, client, http.MethodGet, ts.URL+"/api/resumes", ""); !strings.Contains(body, fmt.Sprintf(`"id":%d`, id)) {
		t.Errorf("list: %s", body)
	}

	// Keywords for a job: no extracted skills yet, so common terms from the posting.
	_, body = do(t, client, http.MethodGet, fmt.Sprintf("%s/api/jobs/%d/keywords", ts.URL, testJobIDs[0]), "")
	if !strings.Contains(body, `"matched":["go","kubernetes"]`) {
		t.Errorf("keywords: %s", body)
	}

	// Another user cannot see it.
	ts2, _ := testServer(t, nil)
	if code, _ := do(t, client, http.MethodGet, fmt.Sprintf("%s/api/resumes/%d", ts2.URL, id), ""); code != 404 {
		t.Errorf("another user's resume: want 404, got %d", code)
	}
}

func TestTailorResumeForAJob(t *testing.T) {
	needTypst(t)
	ts, _ := llmServer(t)
	if code, _ := do(t, client, http.MethodPost, fmt.Sprintf("%s/api/jobs/%d/resume", ts.URL, testJobIDs[0]), ""); code != 400 {
		t.Errorf("tailoring without a resume: want 400, got %d", code)
	}
	if code, _ := importSample(t, ts.URL, false); code != 201 {
		t.Fatal("import failed")
	}
	jobURL := fmt.Sprintf("%s/api/jobs/%d/resume", ts.URL, testJobIDs[0])
	code, body := do(t, client, http.MethodPost, jobURL, "")
	if code != 200 {
		t.Fatalf("tailor: %d %s", code, body)
	}
	var out struct {
		Resume struct {
			ID    int64    `json:"id"`
			JobID *int64   `json:"job_id"`
			Notes []string `json:"notes"`
			Data  struct {
				Sections []struct {
					Entries []struct {
						Bullets []struct{ Text string } `json:"bullets"`
					} `json:"entries"`
				} `json:"sections"`
			} `json:"data"`
			Coverage *struct{ Percent int } `json:"coverage"`
		} `json:"resume"`
	}
	json.Unmarshal([]byte(body), &out)
	r := out.Resume
	bullets := r.Data.Sections[2].Entries[0].Bullets
	if r.JobID == nil || *r.JobID != testJobIDs[0] || r.Coverage == nil {
		t.Fatalf("tailored copy: %+v", r)
	}
	if bullets[0].Text != "Cut deploy time by 50% with GitHub Actions pipelines across 12 services." {
		t.Errorf("a rewrite inventing a number was applied: %q", bullets[0].Text)
	}
	if bullets[1].Text != "Ran Kubernetes on-call for 2,000 requests per second" {
		t.Errorf("a valid rewrite was not applied: %q", bullets[1].Text)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "Kept your original wording in 1 place") {
		t.Errorf("notes: %v", r.Notes)
	}
	if code, _ := do(t, client, http.MethodGet, jobURL, ""); code != 200 {
		t.Errorf("get tailored: %d", code)
	}
	// The base resume is untouched.
	_, list := do(t, client, http.MethodGet, ts.URL+"/api/resumes", "")
	if strings.Count(list, `"id":`) != 2 {
		t.Errorf("want base + tailored: %s", list)
	}
}

func TestImportResumeFromProfile(t *testing.T) {
	ts, _ := llmServer(t)
	pool := testPool(t)
	if code, _ := do(t, client, http.MethodPost, ts.URL+"/api/resumes/import", `{"source":"profile"}`); code != 400 {
		t.Errorf("no resume uploaded: want 400, got %d", code)
	}
	pool.Exec(context.Background(), `INSERT INTO profiles (user_id, resume_text) VALUES ($1, 'Test User\nSRE intern at Acme 2024.')
		ON CONFLICT (user_id) DO UPDATE SET resume_text = EXCLUDED.resume_text`, testUserID)
	code, body := do(t, client, http.MethodPost, ts.URL+"/api/resumes/import", `{"source":"profile"}`)
	if code != 201 || !strings.Contains(body, `"name":"Test User"`) {
		t.Errorf("import from profile: %d %.300s", code, body)
	}
	if code, _ := do(t, client, http.MethodPost, ts.URL+"/api/resumes/import", `{"source":"pdf"}`); code != 400 {
		t.Errorf("unknown source: want 400, got %d", code)
	}
}

func TestResumeTemplatesLaTeXAndReview(t *testing.T) {
	needTypst(t)
	ts, _ := testServer(t, nil)
	code, doc := importSample(t, ts.URL, true)
	if code != 201 {
		t.Fatalf("import: %d", code)
	}
	id := int64(doc["id"].(float64))
	data := doc["data"].(map[string]any)

	code, body := getBody(t, ts.URL+"/api/resumes/templates")
	if code != 200 || !strings.Contains(body, `"id":"parse"`) || !strings.Contains(body, `"id":"signal"`) {
		t.Fatalf("templates: %d %s", code, body)
	}

	// Switch to Signal with an accent: kept on save, used by the downloads.
	url := fmt.Sprintf("%s/api/resumes/%d", ts.URL, id)
	b, _ := json.Marshal(map[string]any{"data": data, "layout": map[string]any{"template": "signal", "accent": "#0F766E", "paper": "a4"}})
	if code, body := do(t, client, http.MethodPut, url, string(b)); code != 200 || !strings.Contains(body, `"template":"signal"`) || !strings.Contains(body, `"accent":"#0f766e"`) {
		t.Fatalf("save template: %d %.300s", code, body)
	}
	code, tex := getBody(t, url+"/tex")
	if code != 200 || !strings.Contains(tex, "% Signal:") || !strings.Contains(tex, "a4paper") || !strings.Contains(tex, "{0F766E}") {
		t.Errorf("tex: %d %.400s", code, tex)
	}
	code, txt := getBody(t, url+"/txt")
	if code != 200 || !strings.Contains(txt, "Ada Lovelace") || strings.Contains(txt, `\`) {
		t.Errorf("txt: %d %.200s", code, txt)
	}

	// LaTeX for unsaved edits, then edited LaTeX back into a resume.
	b, _ = json.Marshal(map[string]any{"data": data, "layout": map[string]any{"template": "ledger"}})
	code, body = do(t, client, http.MethodPost, ts.URL+"/api/resumes/tex", string(b))
	var texOut struct{ TeX, Preamble, Body string }
	json.Unmarshal([]byte(body), &texOut)
	if code != 200 || !strings.Contains(texOut.Preamble, "% Ledger:") || !strings.HasPrefix(texOut.Body, `\resumeHeader{Ada Lovelace}`) {
		t.Fatalf("tex of edits: %d preamble=%q", code, texOut.Preamble[:min(len(texOut.Preamble), 300)])
	}
	edited := strings.Replace(texOut.Body, `\resumeHeader{Ada Lovelace}`, `\resumeHeader{Ada King}`, 1)
	b, _ = json.Marshal(map[string]string{"tex": edited})
	code, body = do(t, client, http.MethodPost, ts.URL+"/api/resumes/parse-tex", string(b))
	if code != 200 || !strings.Contains(body, `"name":"Ada King"`) || !strings.Contains(body, `"warnings":[]`) {
		t.Errorf("parse-tex: %d %.300s", code, body)
	}
	b, _ = json.Marshal(map[string]string{"tex": `\section{Only}`})
	if code, _ := do(t, client, http.MethodPost, ts.URL+"/api/resumes/parse-tex", string(b)); code != 422 {
		t.Errorf("unreadable tex: want 422, got %d", code)
	}

	// Previews carry the review; thumbnails come in every template.
	b, _ = json.Marshal(map[string]any{"data": data, "layout": map[string]any{}})
	code, body = do(t, client, http.MethodPost, ts.URL+"/api/resumes/preview", string(b))
	if code != 200 || !strings.Contains(body, `"review":{"score":`) {
		t.Errorf("preview review: %d %.200s", code, body[:min(len(body), 200)])
	}
	code, body = do(t, client, http.MethodPost, ts.URL+"/api/resumes/thumbnails", string(b))
	var th struct{ Thumbnails map[string]string }
	json.Unmarshal([]byte(body), &th)
	if code != 200 || len(th.Thumbnails) != 4 || !strings.HasPrefix(strings.TrimSpace(th.Thumbnails["parse"]), "<svg") {
		t.Errorf("thumbnails: %d %d", code, len(th.Thumbnails))
	}
}

func TestSuggestBullet(t *testing.T) {
	ts, _ := llmServer(t)
	b, _ := json.Marshal(map[string]string{"text": "Responsible for deploys, which got 50% faster across 12 services", "role": "SRE at Engines Inc"})
	code, body := do(t, client, http.MethodPost, ts.URL+"/api/resumes/suggest-bullet", string(b))
	var out struct{ Suggestions []string }
	json.Unmarshal([]byte(body), &out)
	if code != 200 || len(out.Suggestions) != 2 {
		t.Fatalf("suggestions: %d %s", code, body)
	}
	if out.Suggestions[0] != "Cut deploy time by 50% across 12 services, with GitHub Actions" || !strings.Contains(out.Suggestions[1], "[N]") {
		t.Errorf("cleaning or placeholders wrong: %q", out.Suggestions)
	}
}

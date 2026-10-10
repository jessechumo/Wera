// Package profile turns what a user gives at signup (a resume and a short
// questionnaire) into the profile text the scorer uses.
package profile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// MaxResumeBytes is the largest accepted resume upload.
const MaxResumeBytes = 5 << 20

// maxResumeChars bounds the extracted text kept and sent to the LLM.
const maxResumeChars = 30000

// ErrNotPDF and ErrNoText are user-facing extraction failures.
var (
	ErrNotPDF = errors.New("that file is not a PDF")
	ErrNoText = errors.New("no text could be read from this PDF (is it a scanned image?); paste your resume text instead")
)

var (
	spaceRun = regexp.MustCompile(`[ \t\f\v]+`)
	blankRun = regexp.MustCompile(`\n{3,}`)
)

// ExtractPDFText returns the plain text of a PDF resume. It uses
// poppler's pdftotext when it is installed (the container image ships it;
// it copes with far more PDFs) and falls back to a pure-Go parser.
func ExtractPDFText(data []byte) (string, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", ErrNotPDF
	}
	if text, err := popplerText(data); err == nil {
		text = CleanText(text)
		if hasEnoughText(text) {
			return text, nil
		}
		return "", ErrNoText
	}
	return goPDFText(data)
}

// popplerText runs pdftotext on the PDF bytes (stdin to stdout).
func popplerText(data []byte) (string, error) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-q", "-enc", "UTF-8", "-nopgbrk", "-", "-") //nolint:gosec // fixed binary and arguments; the PDF goes in on stdin
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return out.String(), nil
}

func hasEnoughText(s string) bool {
	return utf8.RuneCountInString(strings.Join(strings.Fields(s), "")) >= 50
}

// goPDFText extracts text with the pure-Go parser.
func goPDFText(data []byte) (text string, err error) {
	// The PDF parser panics on some malformed files; treat that as a
	// failed extraction rather than crashing the request.
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("could not read this PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read this PDF: %w", err)
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		b.WriteString(layoutText(p.Content().Text))
		b.WriteByte('\n')
	}
	text = CleanText(b.String())
	if !hasEnoughText(text) {
		return "", ErrNoText
	}
	return text, nil
}

// layoutText rebuilds lines from positioned glyphs: glyphs on the same
// baseline (within a small tolerance) form a line, lines run top to
// bottom, glyphs left to right, and a horizontal gap wider than a quarter
// of the font size becomes a space. (The library's own row grouping puts
// every glyph on one row for some PDFs.)
func layoutText(glyphs []pdf.Text) string {
	type line struct {
		y      float64
		glyphs []pdf.Text
	}
	var lines []*line
	for _, g := range glyphs {
		if g.S == "" {
			continue
		}
		var into *line
		for _, l := range lines {
			if math.Abs(l.y-g.Y) <= 2 {
				into = l
				break
			}
		}
		if into == nil {
			into = &line{y: g.Y}
			lines = append(lines, into)
		}
		into.glyphs = append(into.glyphs, g)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].y > lines[j].y })

	var b strings.Builder
	for _, l := range lines {
		sort.SliceStable(l.glyphs, func(i, j int) bool { return l.glyphs[i].X < l.glyphs[j].X })
		end := math.Inf(-1)
		for _, g := range l.glyphs {
			if gap := g.X - end; gap > g.FontSize*0.25 && end != math.Inf(-1) && !strings.HasPrefix(g.S, " ") {
				b.WriteByte(' ')
			}
			b.WriteString(g.S)
			end = g.X + g.W
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// CleanText normalizes whitespace in resume text (uploaded or pasted) and
// caps its length.
func CleanText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	s = blankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxResumeChars {
		s = string([]rune(s)[:maxResumeChars])
	}
	return s
}

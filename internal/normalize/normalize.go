// Package normalize converts raw fetched postings into the plain text and
// stable hashes stored in the database.
package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// script/style bodies are dropped entirely before tag stripping.
	scriptRE = regexp.MustCompile(`(?is)<\s*script[^>]*>.*?<\s*/\s*script\s*>`)
	styleRE  = regexp.MustCompile(`(?is)<\s*style[^>]*>.*?<\s*/\s*style\s*>`)
	brRE     = regexp.MustCompile(`(?i)<\s*br\s*/?>`)
	// Paragraph-level closing tags become blank lines (paragraph breaks).
	paraEndRE = regexp.MustCompile(`(?i)</\s*(p|div|h[1-6]|pre|blockquote|section|article|table|tr|ul|ol)\s*>`)
	// List-item closers become single newlines, keeping items tight.
	itemEndRE = regexp.MustCompile(`(?i)</\s*(li|dt|dd)\s*>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRE   = regexp.MustCompile(`[ \t\r\f\v\x{00A0}]+`)
	// hiddenRE matches an element styled out of sight: how a posting hides
	// lines meant only for AI tools.
	hiddenRE = regexp.MustCompile(`(?is)<(?:span|div|p|font|small|li|td|section|b|i|em|strong)\b[^>]*\bstyle\s*=\s*["'][^"']*(?:display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*[0-2](?:\.\d+)?(?:px|pt|em|rem|%)?\s*(?:;|["'])|opacity\s*:\s*0(?:\.0\d*)?\s*(?:;|["'])|color\s*:\s*(?:transparent|#fff(?:fff)?\b|white\b|rgb\(\s*255\s*,\s*255\s*,\s*255\s*\))|(?:left|top|text-indent)\s*:\s*-\d{3,}(?:\.\d+)?px)[^>]*>.*?</(?:span|div|p|font|small|li|td|section|b|i|em|strong)\s*>`)
)

// HTMLToText converts HTML to plain text: entities are unescaped, tags are
// stripped, paragraph boundaries become blank lines, and whitespace is
// collapsed.
//
// Greenhouse serves its content field as HTML-escaped HTML, so the input
// may need one unescape before real tags appear; a second unescape after
// stripping handles entities that were part of the visible text.
func HTMLToText(s string) string {
	if s == "" {
		return ""
	}
	s = html.UnescapeString(s) // reveal the real markup
	s = scriptRE.ReplaceAllString(s, " ")
	s = styleRE.ReplaceAllString(s, " ")
	s = hiddenRE.ReplaceAllString(s, " ") // what a person cannot see on the page is no part of the job
	s = brRE.ReplaceAllString(s, "\n")
	s = paraEndRE.ReplaceAllString(s, "\n\n")
	s = itemEndRE.ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "") // inline tags: no added whitespace
	s = html.UnescapeString(s)        // entities that were visible text

	lines := strings.Split(s, "\n")
	var b strings.Builder
	blank := false
	for _, ln := range lines {
		ln = spaceRE.ReplaceAllString(ln, " ")
		ln = strings.TrimSpace(ln)
		if ln == "" {
			blank = true
			continue
		}
		if b.Len() > 0 && blank {
			b.WriteString("\n") // keep paragraph breaks as blank lines
		}
		b.WriteString(ln)
		b.WriteString("\n")
		blank = false
	}
	return strings.TrimRight(b.String(), "\n")
}

// Clip shortens s to at most n bytes without cutting a character in half.
// Postgres refuses text that ends partway through one.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Text removes what Postgres refuses to store: NUL bytes and invalid UTF-8.
func Text(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
}

// ContentHash returns the sha256 of lowercased title + location +
// description, used to detect changed postings and dedupe.
func ContentHash(title, location, description string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(title)) + "|" +
		strings.TrimSpace(location) + "|" + description))
	return hex.EncodeToString(sum[:])
}

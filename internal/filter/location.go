package filter

import (
	"regexp"
	"strings"
)

// looksNonUS reports whether a location is clearly outside the US.
// US positives (states, cities, "United States", "Remote - US") keep the
// job; ambiguous locations (e.g. plain "Remote") also keep it — the LLM
// confirms later. Only known non-US markers drop the job.
func (e *Engine) looksNonUS(location string) bool {
	loc := strings.TrimSpace(location)
	if loc == "" {
		return false
	}
	for _, re := range e.usPositive {
		if re.MatchString(loc) {
			return false
		}
	}
	for _, re := range e.nonUS {
		if re.MatchString(loc) {
			return true
		}
	}
	return false
}

// Tag turns matched text into a short lowercase reason tag:
// "Senior" -> "senior", "U.S. persons" -> "us_persons".
func Tag(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastUnderscore = false
		} else if b.Len() > 0 && !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

var sentenceRE = regexp.MustCompile(`[^.!?\n]+[.!?]*`)

// sentenceFor returns the sentence in text containing the first match of
// re, as evidence stored with the exclude.
func sentenceFor(re *regexp.Regexp, text string) string {
	for _, s := range sentenceRE.FindAllString(text, -1) {
		if re.MatchString(s) {
			return strings.TrimSpace(s)
		}
	}
	// Fallback: 120 chars around the match.
	loc := re.FindStringIndex(text)
	if loc == nil {
		return text
	}
	start, end := loc[0]-60, loc[1]+60
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

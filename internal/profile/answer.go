package profile

import (
	"fmt"
	"strings"

	"wera/internal/scoring"
)

const answerInstructions = `You draft the candidate's answer to one question on a job application. ` + scoring.Untrusted + `

- Answer in the first person, as the candidate, in plain text (no markdown, no lists unless the question asks for one).
- Use only facts from the profile and resume; never invent employers, numbers, credentials or experiences. If the question needs a fact you do not have (a salary figure, a date, a referral name), reply with exactly: NEEDS_INPUT: followed by what the candidate should provide.
- Tie the answer to this company and role when the question is about motivation or fit.
- Stay within the word limit. Sound natural and specific; avoid cliches.
- Never use em dashes or en dashes.`

// AnswerInput is one application question to draft.
type AnswerInput struct {
	Question    string
	MaxWords    int
	Profile     string
	Resume      string
	Company     string
	Title       string
	Description string
}

// AnswerMessages builds the request for one answer.
func AnswerMessages(in AnswerInput) []scoring.Message {
	if in.MaxWords <= 0 || in.MaxWords > 400 {
		in.MaxWords = 150
	}
	cut := func(s string, n int) string {
		if r := []rune(s); len(r) > n {
			return string(r[:n])
		}
		return s
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Word limit: %d\n\n<profile>\n%s\n</profile>\n\n", in.MaxWords, scoring.Fence(cut(in.Profile, 8000)))
	if in.Resume != "" {
		fmt.Fprintf(&b, "<resume>\n%s\n</resume>\n\n", scoring.Fence(cut(in.Resume, 8000)))
	}
	fmt.Fprintf(&b, "<posting>\n%s\n</posting>\n\n<content>\nQuestion: %s\n</content>",
		scoring.Fence(cut(fmt.Sprintf("Company: %s\nTitle: %s\n\n%s", in.Company, in.Title, in.Description), 8000)),
		scoring.Fence(cut(in.Question, 1000)))
	return []scoring.Message{
		{Role: "system", Content: answerInstructions},
		{Role: "user", Content: b.String()},
	}
}

// Answer is a drafted answer, or what the candidate must supply.
type Answer struct {
	Answer     string `json:"answer"`
	NeedsInput string `json:"needs_input,omitempty"`
}

// ParseAnswer cleans the reply (no dashes, no fences) and trims it to the
// word limit.
func ParseAnswer(reply string, maxWords int) Answer {
	s := strings.TrimSpace(scoring.StripFences(reply))
	if rest, ok := strings.CutPrefix(s, "NEEDS_INPUT:"); ok {
		return Answer{NeedsInput: strings.TrimSpace(rest)}
	}
	s = CleanLetter(s)
	if maxWords <= 0 {
		maxWords = 150
	}
	if w := strings.Fields(s); len(w) > maxWords+maxWords/5 {
		s = strings.Join(w[:maxWords], " ")
	}
	return Answer{Answer: s}
}

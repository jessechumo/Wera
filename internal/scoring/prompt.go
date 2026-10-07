package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// systemPromptText is the fixed scoring instruction from PLAN.md
// section 7.4. It is byte-identical on every call so the prompt prefix
// stays cached.
const systemPromptText = `You evaluate job postings for one candidate. Return ONLY a JSON object, no prose, no markdown fences, matching this schema exactly. Be strict and literal: base sponsorship, years_required, and us_eligible only on what the posting says. If the posting does not mention sponsorship, use "unknown". sponsorship_quote must be copied verbatim from the posting or be null. Score fit 0-100 for THIS candidate considering role type, seniority, required skills, and location. Entry/junior infrastructure, SRE, platform, DevOps, trading-operations, and ML-infrastructure roles that match the candidate's skills should score highest. Penalize roles requiring 4+ years, deep specialization the candidate lacks, or non-infrastructure work.`

// systemPrompt is the full fixed instruction: the prose plus the exact
// JSON schema the reply must match (field names and types matter).
const systemPrompt = systemPromptText + `

Your reply must be exactly this JSON shape (types matter: fit_score is an integer 0-100; years_required is an integer or null; us_eligible is a boolean or null; sponsorship_quote is a string copied verbatim from the posting or null; skills_matched and skills_missing are arrays of strings):

` + schemaJSON

// maxDescriptionChars is the plain-text description truncation limit.
const maxDescriptionChars = 12000

// Message is one chat message in a Coral request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ProfileHash returns the sha256 hex digest of the candidate profile.
func ProfileHash(profile []byte) string {
	sum := sha256.Sum256(profile)
	return hex.EncodeToString(sum[:])
}

// CacheKey returns the prompt_cache_key shared by every scoring call for
// one profile version: "wera-profile-" + the first 16 hex chars of the
// profile sha256 (PLAN.md section 7.4).
func CacheKey(profileHash string) string {
	short := profileHash
	if len(short) > 16 {
		short = short[:16]
	}
	return "wera-profile-" + short
}

// Job is one posting to score, with everything the prompt needs.
type Job struct {
	ID          int64
	Company     string
	Title       string
	Location    string
	URL         string
	Description string
}

// BuildMessages assembles the chat messages: system prompt, profile, job.
// The system and profile messages must be byte-identical on every call and
// come before the job so the long shared prefix is a cached read; only the
// last message varies.
func BuildMessages(profile []byte, j Job) []Message {
	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "CANDIDATE PROFILE:\n" + string(profile)},
		{Role: "user", Content: buildJobMessage(j)},
	}
}

// buildJobMessage renders the per-job part of the prompt.
func buildJobMessage(j Job) string {
	var b strings.Builder
	b.WriteString("JOB:\n")
	fmt.Fprintf(&b, "Company: %s\n", j.Company)
	fmt.Fprintf(&b, "Title: %s\n", j.Title)
	fmt.Fprintf(&b, "Location: %s\n", j.Location)
	fmt.Fprintf(&b, "URL: %s\n", j.URL)
	b.WriteString("\nDescription:\n")
	desc := j.Description
	if len(desc) > maxDescriptionChars {
		desc = desc[:maxDescriptionChars]
	}
	b.WriteString(desc)
	return b.String()
}

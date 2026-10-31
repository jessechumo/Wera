package scoring

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
)

// Version is the scoring method. Analyses are stored under ScoreKey, so
// a new version scores every job again instead of reusing old numbers.
//
// Version 1 asked the model for one 0-100 number. Models anchor on a few
// favourite values (every "strong" job came back exactly 88, all "good"
// jobs shared seven numbers), so the score could not rank the best jobs.
// Version 2 asks for 0-10 ratings of each part of fit against fixed
// anchors and computes the score in code (Ratings.Score).
const Version = 2

// ScoreKey is the profile_hash score analyses are stored under: the
// profile text's hash, tied to the scoring method.
func ScoreKey(profileHash string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("score-v%d\n%s", Version, profileHash)))
	return hex.EncodeToString(sum[:])
}

// ScoreKeys are every key a profile text's scores may be stored under, one
// for each scoring method so far (version 1 used the hash itself).
func ScoreKeys(profileHash string) []string {
	return []string{profileHash, ScoreKey(profileHash)}
}

// Ratings are the model's judgments of one job for one candidate, each
// 0-10 against the anchors in rubricText.
type Ratings struct {
	Role      int  `json:"role"`
	Seniority int  `json:"seniority"`
	Skills    int  `json:"skills"`
	Domain    int  `json:"domain"`
	Location  int  `json:"location"`
	Blocker   bool `json:"blocker"`
}

// rubricText is the part of the scoring prompts that defines each rating.
const rubricText = `Rate each part of fit from 0 to 10, using the whole range and these anchors:
- role: is this the kind of work the candidate targets? 10 = exactly a target role; 7 = a closely related role; 4 = the same field but a different role; 0 = unrelated work.
- seniority: does the level match? 10 = the candidate's level; 6 = one step above or below; 3 = two steps off; 0 = far off (an intern for a senior, a director for a junior).
- skills: share of the required skills and tools the candidate shows. 10 = nearly all; 7 = most; 5 = about half; 2 = few; 0 = none.
- domain: industry and product area. 10 = an industry or domain the candidate has worked in or targets; 5 = neutral; 0 = one they avoid or that needs deep domain expertise they lack.
- location: 10 = where or how the candidate wants to work (city, remote, hybrid); 5 = workable; 0 = a location or work mode they cannot do.
- blocker: true only if the job requires something the candidate clearly cannot meet (a license or certification they lack, a security clearance, citizenship), else false.`

// Score is the 0-100 fit: a weighted mean of the ratings, the skills
// rating blended with the measured coverage of the job's required skills
// (coverage < 0 when unknown), scaled down smoothly for work that is far
// from the candidate's role or level (a one-point rating change never
// jumps the score) and sharply for work they cannot do (scaled, not
// capped, so such jobs keep their order instead of tying). The continuous
// coverage separates jobs the model rates alike.
func (r Ratings) Score(coverage float64) int {
	skills := float64(r.Skills)
	if coverage >= 0 {
		skills = 0.6*skills + 0.4*10*coverage
	}
	s := 10 * (0.35*float64(r.Role) + 0.25*skills + 0.20*float64(r.Seniority) + 0.10*float64(r.Domain) + 0.10*float64(r.Location))
	s *= 0.6 + 0.4*math.Min(float64(r.Role), 5)/5
	s *= 0.7 + 0.3*math.Min(float64(r.Seniority), 4)/4
	if r.Blocker {
		s *= 0.4
	}
	return int(math.Round(math.Max(0, math.Min(100, s))))
}

// Valid reports whether every rating is within 0-10.
func (r Ratings) Valid() bool {
	for _, v := range []int{r.Role, r.Seniority, r.Skills, r.Domain, r.Location} {
		if v < 0 || v > 10 {
			return false
		}
	}
	return true
}

// VerdictFor is the verdict a score earns, the same bands the dashboard
// and extension color by.
func VerdictFor(score int) string {
	switch {
	case score >= 80:
		return "strong"
	case score >= 65:
		return "good"
	case score >= 45:
		return "stretch"
	default:
		return "poor"
	}
}

// Coverage is the share of a job's required skills the candidate shows,
// or -1 when the job names too few to say.
func Coverage(matched, missing []string) float64 {
	n := len(matched) + len(missing)
	if n < 3 {
		return -1
	}
	return float64(len(matched)) / float64(n)
}

// Package relevance ranks jobs against a profile locally (TF-IDF cosine,
// no LLM): it orders the LLM scoring queue so the most promising jobs are
// scored first, and gives new users an estimated ranking the moment their
// profile is saved.
package relevance

import (
	"math"
	"strings"
	"unicode"
)

// Doc is one job to rank. Title words count titleWeight times.
type Doc struct {
	ID    int64
	Title string
	Body  string // description excerpt (or the job's digest)
}

const (
	titleWeight = 3
	maxBodyRune = 4000 // enough for the role summary and requirements
)

// phrases are normalized before tokenizing so they survive as one term.
var phrases = strings.NewReplacer(
	"c++", " cplusplus ", "c#", " csharp ", ".net", " dotnet ", "node.js", " nodejs ",
	"next.js", " nextjs ", "react.js", " react ", "vue.js", " vue ", "ci/cd", " cicd ",
	"machine learning", " machinelearning ", "deep learning", " deeplearning ",
	"data science", " datascience ", "computer vision", " computervision ",
	"site reliability", " sre ", "full stack", " fullstack ", "full-stack", " fullstack ",
	"back end", " backend ", "back-end", " backend ", "front end", " frontend ", "front-end", " frontend ",
)

// Tokens lowercases, normalizes common tech phrases, and drops stopwords
// and one-letter tokens (except "r" and "c", which are languages).
func Tokens(s string) []string {
	s = phrases.Replace(strings.ToLower(s))
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#'
	})
	out := fields[:0]
	for _, f := range fields {
		f = strings.Trim(f, "+#")
		if (len(f) < 2 && f != "r" && f != "c") || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func termFreq(doc Doc) map[string]float64 {
	tf := map[string]float64{}
	for _, t := range Tokens(doc.Title) {
		tf[t] += titleWeight
	}
	body := doc.Body
	if r := []rune(body); len(r) > maxBodyRune {
		body = string(r[:maxBodyRune])
	}
	for _, t := range Tokens(body) {
		tf[t]++
	}
	return tf
}

// Rank scores every doc against the profile and returns estimated scores
// in 35..85: the cosine similarity of TF-IDF vectors (IDF over the docs),
// scaled so the best match gets 85 and the weakest 35. Scaling by
// similarity (not rank) keeps close matches apart: with thousands of
// candidates, rank put the top hundred all at 84 or 85. Estimates only
// order jobs and preview them; the LLM score replaces them.
func Rank(profile string, docs []Doc) map[int64]int {
	if len(docs) == 0 {
		return map[int64]int{}
	}
	sims := Similarities(profile, docs)
	if sims == nil {
		out := make(map[int64]int, len(docs))
		for _, d := range docs {
			out[d.ID] = 50
		}
		return out
	}
	return Scale(sims)
}

// Similarities returns each doc's TF-IDF cosine similarity to the profile
// (IDF over the docs), or nil when the profile has no usable words.
func Similarities(profile string, docs []Doc) map[int64]float64 {
	tfs := make([]map[string]float64, len(docs))
	df := map[string]float64{}
	for i, d := range docs {
		tfs[i] = termFreq(d)
		for t := range tfs[i] {
			df[t]++
		}
	}
	n := float64(len(docs))
	idf := func(t string) float64 { return math.Log(1 + n/(1+df[t])) }

	weigh := func(tf map[string]float64) (map[string]float64, float64) {
		v := make(map[string]float64, len(tf))
		var norm float64
		for t, f := range tf {
			w := (1 + math.Log(f)) * idf(t) // sublinear TF
			v[t] = w
			norm += w * w
		}
		return v, math.Sqrt(norm)
	}
	pv, pnorm := weigh(termFreq(Doc{Body: profile}))
	if pnorm == 0 {
		return nil
	}

	sims := make(map[int64]float64, len(docs))
	for i, d := range docs {
		jv, jnorm := weigh(tfs[i])
		var dot float64
		for t, w := range jv {
			dot += w * pv[t]
		}
		sim := 0.0
		if jnorm > 0 {
			sim = dot / (jnorm * pnorm)
		}
		sims[d.ID] = sim
	}
	return sims
}

// SkillOverlap returns the share of a job's required skills whose words
// all appear in the profile text (0.5 when the job lists none, so a
// missing list neither helps nor hurts).
func SkillOverlap(profile map[string]bool, skills []string) float64 {
	var listed, held float64
	for _, s := range skills {
		toks := Tokens(s)
		if len(toks) == 0 {
			continue
		}
		listed++
		all := true
		for _, t := range toks {
			if !profile[t] {
				all = false
				break
			}
		}
		if all {
			held++
		}
	}
	if listed == 0 {
		return 0.5
	}
	return held / listed
}

// TokenSet is the set of Tokens in s.
func TokenSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, t := range Tokens(s) {
		m[t] = true
	}
	return m
}

// Standardize rescales values to mean 0 and standard deviation 1, so
// signals on different scales can be added (all zeros when they are
// equal).
func Standardize(v map[int64]float64) map[int64]float64 {
	out := make(map[int64]float64, len(v))
	if len(v) == 0 {
		return out
	}
	var mean float64
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	var sd float64
	for _, x := range v {
		sd += (x - mean) * (x - mean)
	}
	sd = math.Sqrt(sd / float64(len(v)))
	for id, x := range v {
		if sd > 0 {
			out[id] = (x - mean) / sd
		} else {
			out[id] = 0
		}
	}
	return out
}

// Scale maps similarities onto estimates in 35..85: the best gets 85 and
// the weakest 35, linearly in between.
func Scale(sims map[int64]float64) map[int64]int {
	out := make(map[int64]int, len(sims))
	for id, s := range ScaleExact(sims) {
		out[id] = int(math.Round(s))
	}
	return out
}

// ScaleExact is Scale without the rounding. Thousands of jobs share the 51
// whole estimates, so anything choosing among the best should use this.
func ScaleExact(sims map[int64]float64) map[int64]float64 {
	out := make(map[int64]float64, len(sims))
	first := true
	var lo, hi float64
	for _, s := range sims {
		if first {
			lo, hi, first = s, s, false
		}
		lo, hi = math.Min(lo, s), math.Max(hi, s)
	}
	for id, s := range sims {
		pct := 1.0
		if hi > lo {
			pct = (s - lo) / (hi - lo)
		}
		out[id] = 35 + 50*pct
	}
	return out
}

var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a about above after again all also am an and any are as at be because been
		before being below between both but by can could did do does doing down during each few for from further
		had has have having he her here hers him his how i if in into is it its itself just me more most my no nor
		not now of off on once only or other our ours out over own same she should so some such than that the
		their them then there these they this those through to too under until up very was we were what when where
		which while who whom why will with would you your yours etc ie eg via per within across including include
		includes us team teams work working role roles job jobs company candidate candidates experience years year
		ability strong excellent good great new looking join help using use used well like make build building`) {
		m[w] = true
	}
	return m
}()

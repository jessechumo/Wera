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
	out := make(map[int64]int, len(docs))
	if len(docs) == 0 {
		return out
	}
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
		for _, d := range docs {
			out[d.ID] = 50
		}
		return out
	}

	type scored struct {
		id  int64
		sim float64
	}
	sims := make([]scored, len(docs))
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
		sims[i] = scored{d.ID, sim}
	}
	lo, hi := sims[0].sim, sims[0].sim
	for _, s := range sims {
		lo, hi = math.Min(lo, s.sim), math.Max(hi, s.sim)
	}
	for _, s := range sims {
		pct := 1.0
		if hi > lo {
			pct = (s.sim - lo) / (hi - lo)
		}
		out[s.id] = 35 + int(math.Round(50*pct))
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

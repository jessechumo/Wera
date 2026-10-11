package resume

import (
	"regexp"
	"sort"
	"strings"
)

// synonyms maps spellings to one canonical form, so "k8s" in a posting
// matches "Kubernetes" on a resume.
var synonyms = map[string]string{
	"k8s": "kubernetes", "golang": "go", "postgres": "postgresql", "psql": "postgresql",
	"js": "javascript", "ts": "typescript", "nodejs": "node.js", "node": "node.js", "reactjs": "react",
	"react.js": "react", "vuejs": "vue", "vue.js": "vue", "nextjs": "next.js", "gcp": "google cloud",
	"google cloud platform": "google cloud", "amazon web services": "aws", "azure cloud": "azure",
	"cicd": "ci/cd", "ci cd": "ci/cd", "continuous integration": "ci/cd", "ml": "machine learning",
	"llms": "llm", "large language models": "llm", "rest api": "rest", "restful": "rest", "rest apis": "rest",
	"py": "python", "c++": "cplusplus", "cpp": "cplusplus", "c#": "csharp", "dotnet": ".net",
	"sre": "site reliability", "site reliability engineering": "site reliability", "infra": "infrastructure",
	"observability": "observability", "o11y": "observability", "unix": "linux",
}

var normSpace = regexp.MustCompile(`[^a-z0-9+#./]+`)

func canon(s string) string {
	s = strings.TrimSpace(normSpace.ReplaceAllString(strings.ToLower(s), " "))
	if c, ok := synonyms[s]; ok {
		return c
	}
	return s
}

// tokensWith adds both raw and canonical forms of every 1-3 word phrase.
func phraseSet(text string) map[string]bool {
	words := strings.Fields(normSpace.ReplaceAllString(strings.ToLower(text), " "))
	set := map[string]bool{}
	for n := 1; n <= 3; n++ {
		for i := 0; i+n <= len(words); i++ {
			p := strings.Trim(strings.Join(words[i:i+n], " "), ".,/")
			if p == "" {
				continue
			}
			set[p] = true
			set[canon(p)] = true
			// "ci/cd" and "linux/rhel" style pairs count for each side.
			for _, part := range strings.Split(p, "/") {
				if part != "" {
					set[canon(part)] = true
				}
			}
		}
	}
	return set
}

// Coverage is how a resume covers a posting's keywords.
type Coverage struct {
	Matched []string `json:"matched"`
	Missing []string `json:"missing"`
	Percent int      `json:"percent"`
}

// KeywordCoverage checks which keywords appear in the resume text,
// allowing common synonyms (k8s, golang, postgres...).
func KeywordCoverage(resumeText string, keywords []string) Coverage {
	have := phraseSet(resumeText)
	cov := Coverage{Matched: []string{}, Missing: []string{}}
	seen := map[string]bool{}
	for _, k := range keywords {
		c := canon(k)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if have[c] || have[strings.ToLower(strings.TrimSpace(k))] {
			cov.Matched = append(cov.Matched, k)
		} else {
			cov.Missing = append(cov.Missing, k)
		}
	}
	if n := len(cov.Matched) + len(cov.Missing); n > 0 {
		cov.Percent = 100 * len(cov.Matched) / n
	}
	return cov
}

// knownTerms is a fallback vocabulary for postings Wera has not analyzed
// yet: common skills that a posting can name.
var knownTerms = strings.Split("python,go,golang,java,javascript,typescript,rust,c++,c#,ruby,php,scala,kotlin,swift,sql,bash,"+
	"react,vue,angular,next.js,node.js,django,flask,fastapi,spring,rails,graphql,rest,grpc,"+
	"kubernetes,k8s,docker,terraform,ansible,helm,aws,azure,gcp,google cloud,linux,ci/cd,github actions,jenkins,"+
	"prometheus,grafana,opentelemetry,datadog,splunk,elasticsearch,kafka,rabbitmq,redis,postgresql,postgres,mysql,"+
	"mongodb,dynamodb,snowflake,spark,airflow,dbt,pandas,numpy,pytorch,tensorflow,scikit-learn,machine learning,"+
	"llm,rag,langchain,nlp,computer vision,microservices,distributed systems,system design,networking,security,"+
	"observability,site reliability,incident response,on-call,slos,tdd,unit testing,agile,figma,tableau", ",")

// KnownTerms lists which common skills a text mentions, in first-seen
// order (used when a posting has no extracted skills yet).
func KnownTerms(text string) []string {
	have := phraseSet(text)
	type hit struct {
		term string
		at   int
	}
	lower := strings.ToLower(text)
	var hits []hit
	seen := map[string]bool{}
	for _, t := range knownTerms {
		c := canon(t)
		if seen[c] || !have[c] && !have[t] {
			continue
		}
		seen[c] = true
		at := strings.Index(lower, t)
		if at < 0 {
			at = len(lower)
		}
		hits = append(hits, hit{t, at})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.term)
	}
	return out
}

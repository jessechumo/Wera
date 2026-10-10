package scoring

import (
	"errors"
	"testing"
)

const validDeepReply = `{
  "why_fit": ["Go and Linux production experience match the platform work.",
              "Kubernetes and Terraform project mirrors the stack.",
              "Trading-operations exposure fits the desk culture."],
  "resume_bullets": ["Ran RHEL production for 200+ hosts.",
                     "Automated deploys with Ansible.",
                     "Built a K8s cluster with Prometheus monitoring."],
  "gaps": [{"gap": "No professional Scala", "address": "Highlight fast language pickup via Python/Go."}],
  "interview_topics": ["Linux troubleshooting live", "Incident response", "K8s networking"]
}`

func TestParseDeepAnalysisValid(t *testing.T) {
	d, err := ParseDeepAnalysis(validDeepReply)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(d.WhyFit) != 3 || len(d.ResumeBullets) != 3 || len(d.InterviewTopics) != 3 {
		t.Errorf("lengths: %+v", d)
	}
	if len(d.Gaps) != 1 || d.Gaps[0].Gap != "No professional Scala" {
		t.Errorf("gaps: %+v", d.Gaps)
	}
}

func TestParseDeepAnalysisFenced(t *testing.T) {
	fenced := "```json\n" + validDeepReply + "\n```\n"
	if _, err := ParseDeepAnalysis(fenced); err != nil {
		t.Fatalf("fenced reply should parse: %v", err)
	}
}

func TestParseDeepAnalysisEmptyGapsAllowed(t *testing.T) {
	d, err := ParseDeepAnalysis(`{
	  "why_fit": ["a"], "resume_bullets": ["b"],
	  "gaps": [], "interview_topics": ["c"]
	}`)
	if err != nil {
		t.Fatalf("empty gaps are valid: %v", err)
	}
	if len(d.Gaps) != 0 {
		t.Errorf("gaps: %+v", d.Gaps)
	}
}

func TestParseDeepAnalysisInvalid(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{"not json", "sure, here is the plan"},
		{"missing why_fit", `{"resume_bullets":["b"],"gaps":[],"interview_topics":["c"]}`},
		{"empty why_fit", `{"why_fit":[],"resume_bullets":["b"],"gaps":[],"interview_topics":["c"]}`},
		{"empty bullet", `{"why_fit":[""],"resume_bullets":["b"],"gaps":[],"interview_topics":["c"]}`},
		{"too many gaps", `{"why_fit":["a"],"resume_bullets":["b"],
		    "gaps":[{"gap":"1","address":"x"},{"gap":"2","address":"x"},{"gap":"3","address":"x"},
		            {"gap":"4","address":"x"},{"gap":"5","address":"x"},{"gap":"6","address":"x"}],
		    "interview_topics":["c"]}`},
		{"gap missing address", `{"why_fit":["a"],"resume_bullets":["b"],
		    "gaps":[{"gap":"1"}],"interview_topics":["c"]}`},
		{"wrong type", `{"why_fit":"a","resume_bullets":["b"],"gaps":[],"interview_topics":["c"]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDeepAnalysis(tc.reply)
			if err == nil {
				t.Fatal("expected schema error")
			}
			if !errors.Is(err, ErrInvalidSchema) {
				t.Errorf("want ErrInvalidSchema, got %v", err)
			}
		})
	}
}

// TestBuildDeepInputCachePrefix checks the M3 caching invariant: the
// profile item is byte-identical across jobs and precedes the job item.
func TestBuildDeepInputCachePrefix(t *testing.T) {
	profile := []byte(testProfile)
	j1 := testJobs(2)[0]
	j2 := testJobs(2)[1]
	j2.Title = j1.Title + " (Senior)"

	in1 := BuildDeepInput(profile, j1)
	in2 := BuildDeepInput(profile, j2)
	if len(in1) != 2 || len(in2) != 2 {
		t.Fatalf("input lengths: %d %d", len(in1), len(in2))
	}
	if in1[0].Content != in2[0].Content {
		t.Error("profile item must be byte-identical across jobs")
	}
	if in1[1].Content == in2[1].Content {
		t.Error("job items should differ between jobs")
	}
	if in1[1].Role != "user" || in1[0].Role != "user" {
		t.Errorf("roles: %+v", in1)
	}
}

package store

import "testing"

func TestNormalizeJobURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://Boards.Greenhouse.io/acme/jobs/123/?utm_source=x&gh_src=y#apply": "https://boards.greenhouse.io/acme/jobs/123",
		"https://jobs.lever.co/acme/abc?lever-source=LinkedIn":                    "https://jobs.lever.co/acme/abc",
		"https://example.com/careers?id=42":                                       "https://example.com/careers?id=42",
		"  not a url ":                                                            "not a url",
	} {
		if got := NormalizeJobURL(in); got != want {
			t.Errorf("NormalizeJobURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplicantValidate(t *testing.T) {
	if err := (&Applicant{NeedsSponsorship: "maybe"}).Validate(); err == nil {
		t.Error("yes/no answers must be yes or no")
	}
	if err := (&Applicant{Phone: string(make([]byte, 301))}).Validate(); err == nil {
		t.Error("long values must be refused")
	}
	if err := (&Applicant{NeedsSponsorship: "yes", City: "Austin"}).Validate(); err != nil {
		t.Error(err)
	}
}

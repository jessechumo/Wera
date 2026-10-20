package sources

import "testing"

func TestBoardFromURL(t *testing.T) {
	cases := []struct{ url, ats, token string }{
		{"https://boards.greenhouse.io/Stripe/jobs/123?gh_src=x", "greenhouse", "stripe"},
		{"https://job-boards.greenhouse.io/anthropic/jobs/4567", "greenhouse", "anthropic"},
		{"https://boards.greenhouse.io/embed/job_app?for=figma&token=89", "greenhouse", "figma"},
		{"https://jobs.lever.co/palantir/0b1c-22/apply", "lever", "palantir"},
		{"https://jobs.ashbyhq.com/openai/abc-def", "ashby", "openai"},
		{"https://jobs.smartrecruiters.com/BoschGroup/7440", "smartrecruiters", "BoschGroup"},
		{"https://nvidia.wd5.myworkdayjobs.com/en-US/NVIDIAExternalCareerSite/job/US-CA-Santa-Clara/Engineer_JR1", "workday", "nvidia.wd5/NVIDIAExternalCareerSite"},
		{"https://ccf.wd1.myworkdayjobs.com/ClevelandClinicCareers/details/RN_R123", "workday", "ccf.wd1/ClevelandClinicCareers"},
		{"https://wd5.myworkdaysite.com/en-US/recruiting/Moog/MoogCareers/job/x", "workday", "moog.wd5/MoogCareers"},
		{"https://apply.workable.com/Thorlabs/j/3BBF817190/apply", "workable", "thorlabs"},
		{"https://ats.rippling.com/riot-platforms-careers/jobs/ad829750", "rippling", "riot-platforms-careers"},
		{"https://jpmc.fa.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1001/job/210674113", "oracle", "jpmc.fa/CX_1001"},
		{"https://hdhe.fa.em3.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX/job/400050636", "oracle", "hdhe.fa.em3/CX"},
	}
	for _, c := range cases {
		ats, token, ok := BoardFromURL(c.url)
		if !ok || ats != c.ats || token != c.token {
			t.Errorf("BoardFromURL(%q) = %q, %q, %v; want %q, %q", c.url, ats, token, ok, c.ats, c.token)
		}
	}
	for _, u := range []string{
		"https://careers.example.com/jobs/1", "https://boards.greenhouse.io/", "https://boards.greenhouse.io/embed/job_app",
		"https://nvidia.wd5.myworkdayjobs.com/", "ftp://jobs.lever.co/acme", "https://jobs.eu.lever.co/acme/1",
		"https://apply.workable.com/j/3BBF817190", "https://jpmc.fa.oraclecloud.com/hcmUI/CandidateExperience/en/my-profile",
	} {
		if ats, token, ok := BoardFromURL(u); ok {
			t.Errorf("BoardFromURL(%q) = %q, %q; want no board", u, ats, token)
		}
	}
}

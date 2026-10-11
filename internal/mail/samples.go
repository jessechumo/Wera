package mail

// Sample returns realistic sample data for a template, for previews.
func Sample(name, publicURL, email, fullName string) any {
	b := NewBase(publicURL, email, fullName)
	jobs := []DigestJob{
		{Title: "Software Engineer, Infrastructure", Company: "Stripe", Location: "Seattle, WA", Score: 91, URL: b.AppURL + "jobs?job=1",
			WorkMode: "Hybrid", Sponsorship: "Sponsors visas", Reason: "Go, Kubernetes and on-call experience line up with the platform team's needs; mid-level, 2+ years."},
		{Title: "Site Reliability Engineer II", Company: "Datadog", Location: "New York, NY", Score: 86, URL: b.AppURL + "jobs?job=2",
			WorkMode: "Onsite", Reason: "Observability stack (Prometheus, Grafana, OpenTelemetry) matches closely."},
		{Title: "Platform Engineer", Company: "Ramp", Location: "Remote, US", Score: 78, URL: b.AppURL + "jobs?job=3",
			WorkMode: "Remote", Sponsorship: "Sponsors visas", Reason: "Terraform and AWS fit; asks for more years than you have."},
	}
	switch name {
	case "verify":
		return Verify{Base: b, Link: b.AppURL + "verify-email?token=sample-token", ExpiresIn: "48 hours"}
	case "reset":
		return Reset{Base: b, Link: b.AppURL + "reset-password?token=sample-token", ExpiresIn: "1 hour"}
	case "welcome":
		return Welcome{Base: b, Companies: 572, Industries: 28, Steps: []Step{
			{"Check Today each morning", "Your best new matches, ranked by fit, with sponsorship and work mode at a glance."},
			{"Tailor your resume per job", "One click makes a one-page copy aimed at the posting, from your real experience."},
			{"Add the Chrome extension", "Save jobs from any site, write cover letters and fill applications. You always submit."},
		}}
	case "digest":
		return Digest{Base: b, Jobs: jobs, Top: &jobs[0], NewCount: 34, Period: "today", Frequency: "daily", TodayURL: b.AppURL}
	case "match":
		return Match{Base: b, MinScore: 85, Job: MatchJob{DigestJob: jobs[0], Skills: []string{"Go", "Kubernetes", "Terraform", "On-call"}}}
	case "test":
		return Test{Base: b, Provider: "smtp", From: "Wera <hello@example.com>"}
	}
	return nil
}

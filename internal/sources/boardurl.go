package sources

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	workdayHost = regexp.MustCompile(`^([a-z0-9-]+)\.(wd\d+)\.myworkdayjobs\.com$`)
	workdaySite = regexp.MustCompile(`^(wd\d+)\.myworkdaysite\.com$`)
	locale      = regexp.MustCompile(`^[a-z]{2}-[A-Za-z]{2}$`)
	slugOK      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	oracleHost  = regexp.MustCompile(`^([a-z0-9-]+\.fa(?:\.[a-z0-9-]+)?)\.oraclecloud\.com$`)
)

// BoardFromURL returns the job board a posting's address belongs to, as
// the ATS name and the token companies.yaml would use for it
// ("greenhouse", "acme"), when the address is on a board Wera can read:
// Greenhouse, Lever, Ashby, SmartRecruiters, Workday, Workable, Rippling
// or Oracle Recruiting Cloud.
// Career sites on a company's own domain report ok=false.
func BoardFromURL(raw string) (ats, token string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	var parts []string
	for _, p := range strings.Split(u.Path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	first := func() string {
		if len(parts) == 0 {
			return ""
		}
		return parts[0]
	}
	slug := func(ats, s string) (string, string, bool) {
		if !slugOK.MatchString(s) {
			return "", "", false
		}
		return ats, s, true
	}

	switch host {
	case "boards.greenhouse.io", "job-boards.greenhouse.io":
		// Embedded boards name theirs in ?for=.
		if first() == "embed" {
			return slug("greenhouse", strings.ToLower(u.Query().Get("for")))
		}
		return slug("greenhouse", strings.ToLower(first()))
	case "jobs.lever.co":
		return slug("lever", strings.ToLower(first()))
	case "jobs.ashbyhq.com":
		return slug("ashby", first())
	case "jobs.smartrecruiters.com", "careers.smartrecruiters.com":
		// Company identifiers are case-sensitive.
		return slug("smartrecruiters", first())
	case "apply.workable.com":
		// apply.workable.com/j/<code> links carry no account.
		if first() == "j" || first() == "api" {
			return "", "", false
		}
		return slug("workable", strings.ToLower(first()))
	case "ats.rippling.com":
		return slug("rippling", strings.ToLower(first()))
	}

	// <host>.oraclecloud.com/hcmUI/CandidateExperience/<lang>/sites/<site>/...
	if m := oracleHost.FindStringSubmatch(host); m != nil {
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] == "sites" && slugOK.MatchString(parts[i+1]) {
				return "oracle", m[1] + "/" + parts[i+1], true
			}
		}
		return "", "", false
	}

	if m := workdayHost.FindStringSubmatch(host); m != nil {
		// /en-US/Site/job/..., /Site/details/..., /wday/cxs/tenant/Site/...
		if len(parts) >= 4 && parts[0] == "wday" && parts[1] == "cxs" {
			parts = parts[3:]
		} else if len(parts) > 0 && locale.MatchString(parts[0]) {
			parts = parts[1:]
		}
		if len(parts) == 0 || !slugOK.MatchString(parts[0]) {
			return "", "", false
		}
		return "workday", m[1] + "." + m[2] + "/" + parts[0], true
	}
	// wd5.myworkdaysite.com/recruiting/tenant/Site/...
	if m := workdaySite.FindStringSubmatch(host); m != nil {
		if len(parts) > 0 && locale.MatchString(parts[0]) {
			parts = parts[1:]
		}
		if len(parts) < 3 || parts[0] != "recruiting" || !slugOK.MatchString(parts[1]) || !slugOK.MatchString(parts[2]) {
			return "", "", false
		}
		return "workday", strings.ToLower(parts[1]) + "." + m[1] + "/" + parts[2], true
	}
	return "", "", false
}

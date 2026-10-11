package mail

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
)

//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

// Base is in every email.
type Base struct {
	AppURL    string // the dashboard
	AppHost   string // its host, for the footer
	ManageURL string // email settings
	Email     string // the recipient
	FirstName string
}

// NewBase fills the shared fields for a recipient.
func NewBase(publicURL, email, name string) Base {
	publicURL = strings.TrimRight(publicURL, "/")
	host := publicURL
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		host = u.Host
	}
	first := strings.TrimSpace(name)
	if i := strings.IndexByte(first, ' '); i > 0 {
		first = first[:i]
	}
	if first == "" {
		first = "there"
	}
	return Base{AppURL: publicURL + "/", AppHost: host, ManageURL: publicURL + "/settings#notifications", Email: email, FirstName: first}
}

// Verify asks to confirm an address.
type Verify struct {
	Base
	Link      string
	ExpiresIn string
}

// Reset carries a password reset link.
type Reset struct {
	Base
	Link      string
	ExpiresIn string
}

// Step is one numbered line of the welcome email.
type Step struct{ Title, Text string }

// Welcome greets a new account.
type Welcome struct {
	Base
	Companies  int
	Industries int
	Steps      []Step
}

// DigestJob is one job in a digest.
type DigestJob struct {
	Title, Company, Location string
	Score                    int
	URL                      string
	WorkMode                 string // "Remote", "Hybrid"...
	Sponsorship              string // "Sponsors visas", or ""
	Reason                   string
}

// Digest lists the best new matches of a period.
type Digest struct {
	Base
	Jobs      []DigestJob
	Top       *DigestJob
	NewCount  int
	Period    string // "today", "this week"
	Frequency string // "daily", "weekly"
	TodayURL  string
}

// MatchJob is the job in a strong-match alert.
type MatchJob struct {
	DigestJob
	Skills []string
}

// Match alerts about one strong match.
type Match struct {
	Base
	Job      MatchJob
	MinScore int
}

// Test checks the setup.
type Test struct {
	Base
	Provider string
	From     string
}

// Names of the templates, and the data type each takes.
var templateData = map[string]any{
	"verify": Verify{}, "reset": Reset{}, "welcome": Welcome{}, "digest": Digest{}, "match": Match{}, "test": Test{},
}

// Templates lists the template names.
func Templates() []string {
	return []string{"verify", "reset", "welcome", "digest", "match", "test"}
}

type buttonData struct{ URL, Label string }

var funcs = map[string]any{
	"button": func(url, label string) buttonData { return buttonData{url, label} },
	"inc":    func(i int) int { return i + 1 },
	"join":   strings.Join,
	"scoreColor": func(score int) string {
		switch {
		case score >= 80:
			return "#1e7a3c"
		case score >= 65:
			return "#3157c4"
		default:
			return "#8a8f99"
		}
	},
}

// Rendered is an email ready to become a Message.
type Rendered struct {
	Subject, HTML, Text string
}

// Render fills a template with its data (one of the types above).
func Render(name string, data any) (Rendered, error) {
	want, ok := templateData[name]
	if !ok {
		return Rendered{}, fmt.Errorf("unknown email template %q", name)
	}
	if fmt.Sprintf("%T", want) != fmt.Sprintf("%T", data) {
		return Rendered{}, fmt.Errorf("template %q takes %T, got %T", name, want, data)
	}
	htmlSrc, err := templateFS.ReadFile("templates/" + name + ".html")
	if err != nil {
		return Rendered{}, err
	}
	layout, _ := templateFS.ReadFile("templates/layout.html")
	textSrc, err := templateFS.ReadFile("templates/" + name + ".txt")
	if err != nil {
		return Rendered{}, err
	}

	h, err := htmltemplate.New("email").Funcs(funcs).Parse(string(layout))
	if err == nil {
		_, err = h.Parse(string(htmlSrc))
	}
	if err != nil {
		return Rendered{}, fmt.Errorf("parse %s: %w", name, err)
	}
	var html bytes.Buffer
	if err := h.ExecuteTemplate(&html, "layout", data); err != nil {
		return Rendered{}, fmt.Errorf("render %s: %w", name, err)
	}

	// Subject and text are plain text: no HTML escaping ("AT&T" stays).
	t, err := texttemplate.New("text").Funcs(funcs).Parse(string(htmlSrc))
	if err == nil {
		_, err = t.Parse(string(textSrc))
	}
	if err != nil {
		return Rendered{}, fmt.Errorf("parse %s text: %w", name, err)
	}
	var subject, text bytes.Buffer
	if err := t.ExecuteTemplate(&subject, "subject", data); err != nil {
		return Rendered{}, err
	}
	if err := t.ExecuteTemplate(&text, "text", data); err != nil {
		return Rendered{}, err
	}
	return Rendered{
		Subject: strings.Join(strings.Fields(subject.String()), " "),
		HTML:    html.String(),
		Text:    strings.TrimSpace(text.String()) + "\n",
	}, nil
}

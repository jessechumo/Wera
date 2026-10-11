package resume

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

//go:embed tex_preamble.tex
var texPreamble string

// --- export ---------------------------------------------------------------

var texEscaper = strings.NewReplacer(
	`\`, `\textbackslash{}`, `{`, `\{`, `}`, `\}`, `$`, `\$`, `&`, `\&`, `#`, `\#`,
	`^`, `\textasciicircum{}`, `_`, `\_`, `%`, `\%`, `~`, `\textasciitilde{}`,
	"–", "--", "—", "---",
)

// esc makes user text safe inside LaTeX: every special character is
// escaped, so a resume can never run LaTeX commands.
func esc(s string) string { return texEscaper.Replace(s) }

// texURL makes a URL safe inside \href's first argument: no character can
// close the argument or start a command.
func texURL(url string) string {
	return strings.NewReplacer(`\`, "", `{`, "%7B", `}`, "%7D", `%`, `\%`, `#`, `\#`).Replace(url)
}

func href(url, text string) string {
	return `\href{` + texURL(url) + `}{` + esc(text) + `}`
}

// ExportTeX writes the resume in the LaTeX template it mirrors, for anyone
// who wants to keep editing it in Overleaf or a local TeX setup.
func (r *Resume) ExportTeX() string {
	var b strings.Builder
	b.WriteString(texPreamble)
	b.WriteString("\n\\begin{document}\n\n%----------HEADING----------\n\\begin{center}\n")
	fmt.Fprintf(&b, "    {\\Huge \\scshape %s} \\\\ \\vspace{1pt}\n", esc(r.Name))
	var parts []string
	if r.Phone != "" {
		parts = append(parts, esc(r.Phone))
	}
	if r.Email != "" {
		parts = append(parts, href("mailto:"+r.Email, r.Email))
	}
	if r.Location != "" {
		parts = append(parts, esc(r.Location))
	}
	for _, l := range r.Links {
		parts = append(parts, href(l.URL, l.Label))
	}
	b.WriteString("    \\small " + strings.Join(parts, " $|$\n    ") + "\n\\end{center}\n")

	for _, s := range r.Sections {
		if s.Hidden {
			continue
		}
		fmt.Fprintf(&b, "\n%%-----------%s-----------\n\\section{%s}\n", strings.ToUpper(esc(s.Title)), esc(s.Title))
		switch s.Kind {
		case KindSkills:
			b.WriteString("\\begin{itemize}[leftmargin=0.15in,label={}]\n\\small{\\item{\n")
			var lines []string
			for _, g := range s.Skills {
				lines = append(lines, fmt.Sprintf("\\textbf{%s:} %s", esc(g.Name), esc(g.Items)))
			}
			b.WriteString(strings.Join(lines, " \\\\\n") + "\n}}\n\\end{itemize}\n")
		case KindSummary:
			b.WriteString("\\small{" + esc(s.Text) + "}\n")
		default:
			b.WriteString("\\resumeSubHeadingListStart\n")
			for _, e := range s.Entries {
				if e.Hidden {
					continue
				}
				switch s.Kind {
				case KindProjects:
					desc := esc(e.Text)
					if e.URL != "" {
						label := e.LinkLabel
						if label == "" {
							label = "link"
						}
						desc += ` \href{` + texURL(e.URL) + `}{\underline{` + esc(label) + `}}`
					}
					fmt.Fprintf(&b, "\n\\resumeProject\n{%s}{%s}\n{%s}\n", esc(e.Heading), esc(e.Tech), desc)
				case KindCompact:
					fmt.Fprintf(&b, "\n\\resumeCompact\n{%s}{%s}{%s}\n", esc(e.Heading), esc(e.Subheading), esc(e.Dates))
				default:
					fmt.Fprintf(&b, "\n\\resumeSubheading\n{%s}{%s}\n{%s}{%s}\n", esc(e.Heading), esc(e.Dates), esc(e.Subheading), esc(e.Location))
				}
				var items []string
				for _, bl := range e.Bullets {
					if !bl.Hidden {
						items = append(items, "\\resumeItem{"+esc(bl.Text)+"}")
					}
				}
				if len(items) > 0 {
					b.WriteString("\\resumeItemListStart\n" + strings.Join(items, "\n") + "\n\\resumeItemListEnd\n")
				}
			}
			b.WriteString("\n\\resumeSubHeadingListEnd\n")
		}
	}
	b.WriteString("\n\\end{document}\n")
	return b.String()
}

// --- import ---------------------------------------------------------------

// readArg reads one {...} argument starting at s[i] (after optional
// whitespace), honoring nested and escaped braces. It returns the inside
// and the index after the closing brace, or ok=false.
func readArg(s string, i int) (string, int, bool) {
	for i < len(s) && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	if i >= len(s) || s[i] != '{' {
		return "", i, false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++ // skip the escaped character
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i+1 : j], j + 1, true
			}
		}
	}
	return "", i, false
}

// call is one macro use: its arguments and where it starts in the body.
type call struct {
	args []string
	at   int
}

// macroCalls finds \name{..}{..}... calls with n arguments, in order.
func macroCalls(body, name string, n int) []call {
	var out []call
	needle := `\` + name
	for i := 0; ; {
		k := strings.Index(body[i:], needle)
		if k < 0 {
			break
		}
		at := i + k + len(needle)
		// \resumeItem must not match \resumeItemListStart.
		if at < len(body) && (body[at] >= 'a' && body[at] <= 'z' || body[at] >= 'A' && body[at] <= 'Z') {
			i = at
			continue
		}
		args := make([]string, 0, n)
		pos := at
		ok := true
		for a := 0; a < n; a++ {
			var v string
			v, pos, ok = readArg(body, pos)
			if !ok {
				break
			}
			args = append(args, v)
		}
		if ok {
			out = append(out, call{args: args, at: i + k})
		}
		i = max(pos, at)
	}
	return out
}

var (
	hrefRE     = regexp.MustCompile(`\\href\{([^}]*)\}\{`)
	cmdArgRE   = regexp.MustCompile(`\\(?:textbf|textit|emph|underline|small|large|Large|Huge|huge|scshape|textsc|texttt|mbox)\b\s*`)
	cmdRE      = regexp.MustCompile(`\\[a-zA-Z]+\*?(\[[^\]]*\])?`)
	vspaceRE   = regexp.MustCompile(`\\vspace\{[^}]*\}|\\hspace\{[^}]*\}`)
	spacesRE   = regexp.MustCompile(`[ \t]+`)
	commentRE  = regexp.MustCompile(`(?m)(^|[^\\])%.*$`)
	sectionRE  = regexp.MustCompile(`\\section\*?\{`)
	skillLine  = regexp.MustCompile(`\\textbf\{([^}]*)\}\s*([^\\]*(?:\\[&%$#_][^\\]*)*)`)
	unescapeRE = strings.NewReplacer(`\&`, "&", `\%`, "%", `\$`, "$", `\_`, "_", `\#`, "#", `\{`, "{", `\}`, "}", "---", "—", "--", "–", "~", " ", `$|$`, "|", `\textbar`, "|", `\\`, " ", `\ `, " ")
)

// plain turns a LaTeX fragment into text: links become their text (the
// first URL is returned too), formatting commands vanish, escapes resolve.
func plain(s string) (text, firstURL string) {
	s = vspaceRE.ReplaceAllString(s, "")
	if m := hrefRE.FindStringSubmatch(s); m != nil {
		firstURL = m[1]
	}
	s = hrefRE.ReplaceAllString(s, "{")
	s = cmdArgRE.ReplaceAllString(s, "")
	s = unescapeRE.Replace(s)
	s = cmdRE.ReplaceAllString(s, "")
	s = strings.NewReplacer("{", "", "}", "").Replace(s)
	s = spacesRE.ReplaceAllString(strings.ReplaceAll(s, "\n", " "), " ")
	return strings.TrimSpace(s), firstURL
}

func p(s string) string {
	t, _ := plain(s)
	return t
}

var digitsRE = regexp.MustCompile(`\d`)

// ImportTeX reads a resume written in the "Jake's resume" family of LaTeX
// templates (\resumeSubheading, \resumeItem, \resumeProject,
// \resumeCompact and a \textbf{Group:} skills block). Anything it cannot
// place is reported in warnings rather than guessed.
func ImportTeX(src string) (*Resume, []string, error) {
	var warnings []string
	src = commentRE.ReplaceAllString(src, "$1")
	if i := strings.Index(src, `\begin{document}`); i >= 0 {
		src = src[i+len(`\begin{document}`):]
	}
	if i := strings.Index(src, `\end{document}`); i >= 0 {
		src = src[:i]
	}
	r := &Resume{}

	// Header: everything before the first \section.
	head := src
	if loc := sectionRE.FindStringIndex(src); loc != nil {
		head = src[:loc[0]]
	}
	if m := regexp.MustCompile(`\\scshape\s+([^}]*)\}`).FindStringSubmatch(head); m != nil {
		r.Name = p(m[1])
	}
	contactBlock := head
	if i := strings.Index(head, `\small`); i >= 0 {
		contactBlock = head[i:]
	}
	if i := strings.Index(contactBlock, `\end{center}`); i >= 0 {
		contactBlock = contactBlock[:i]
	}
	for _, part := range strings.Split(contactBlock, `$|$`) {
		text, url := plain(part)
		switch {
		case url != "" && strings.HasPrefix(url, "mailto:"):
			r.Email = strings.TrimPrefix(url, "mailto:")
		case url != "":
			r.Links = append(r.Links, Link{Label: text, URL: url})
		case strings.Contains(text, "@"):
			r.Email = text
		case len(digitsRE.FindAllString(text, -1)) >= 7:
			r.Phone = text
		case text != "":
			r.Location = text
		}
	}
	if r.Name == "" {
		return nil, nil, errors.New("could not find the name (expected {\\Huge \\scshape Your Name})")
	}

	// Sections.
	locs := sectionRE.FindAllStringIndex(src, -1)
	for k, loc := range locs {
		title, after, ok := readArg(src, loc[1]-1)
		if !ok {
			continue
		}
		end := len(src)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		body := src[after:end]
		sec := Section{ID: newID(), Title: p(title)}
		subs := macroCalls(body, "resumeSubheading", 4)
		projs := macroCalls(body, "resumeProject", 3)
		comps := macroCalls(body, "resumeCompact", 3)
		switch {
		case len(subs) > 0:
			sec.Kind = KindEntries
			for i, c := range subs {
				a := c.args
				e := Entry{ID: newID(), Heading: p(a[0]), Dates: p(a[1]), Subheading: p(a[2]), Location: p(a[3])}
				e.Bullets = bulletsBetween(body, c.at, nextStart(subs, i, len(body)))
				sec.Entries = append(sec.Entries, e)
			}
		case len(projs) > 0:
			sec.Kind = KindProjects
			for i, c := range projs {
				a := c.args
				desc, label, url := splitLink(a[2])
				e := Entry{ID: newID(), Heading: p(a[0]), Tech: p(a[1]), Text: desc, URL: url, LinkLabel: label}
				e.Bullets = bulletsBetween(body, c.at, nextStart(projs, i, len(body)))
				sec.Entries = append(sec.Entries, e)
			}
		case len(comps) > 0:
			sec.Kind = KindCompact
			for _, c := range comps {
				a := c.args
				sec.Entries = append(sec.Entries, Entry{ID: newID(), Heading: p(a[0]), Subheading: p(a[1]), Dates: p(a[2])})
			}
		case strings.Contains(body, `\textbf{`):
			sec.Kind = KindSkills
			for _, m := range skillLine.FindAllStringSubmatch(body, -1) {
				name := strings.TrimSuffix(strings.TrimSpace(p(m[1])), ":")
				if items := p(m[2]); name != "" && items != "" {
					sec.Skills = append(sec.Skills, SkillGroup{ID: newID(), Name: name, Items: items})
				}
			}
		default:
			sec.Kind = KindSummary
			sec.Text = p(body)
			if sec.Text == "" {
				warnings = append(warnings, fmt.Sprintf("section %q was empty or not understood", sec.Title))
				continue
			}
		}
		r.Sections = append(r.Sections, sec)
	}
	if len(r.Sections) == 0 {
		warnings = append(warnings, "no \\section found")
	}
	if err := r.Normalize(); err != nil {
		return nil, warnings, err
	}
	return r, warnings, nil
}

// splitLink separates a trailing \href from a description: the text
// before it, the link's label and its URL.
func splitLink(s string) (desc, label, url string) {
	loc := hrefRE.FindStringSubmatchIndex(s)
	if loc == nil {
		return p(s), "", ""
	}
	url = s[loc[2]:loc[3]]
	inner, _, _ := readArg(s, loc[1]-1)
	return p(s[:loc[0]]), p(inner), url
}

func nextStart(calls []call, i, end int) int {
	if i+1 < len(calls) {
		return calls[i+1].at
	}
	return end
}

// bulletsBetween collects \resumeItem{...} texts between the macro call
// starting at offset from and the next entry.
func bulletsBetween(body string, from, end int) []Bullet {
	chunk := body[from:min(end, len(body))]
	var out []Bullet
	for _, c := range macroCalls(chunk, "resumeItem", 1) {
		if t := p(c.args[0]); t != "" {
			out = append(out, Bullet{ID: newID(), Text: t})
		}
	}
	return out
}

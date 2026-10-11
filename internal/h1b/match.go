package h1b

// Matcher finds which filing employers are a company.
type Matcher struct {
	byKey   map[string][]string // employer key -> itself (exact names)
	byLoose map[string][]string // loose key -> employer keys
}

// NewMatcher indexes the employer keys present in the data.
func NewMatcher(employerKeys []string) *Matcher {
	m := &Matcher{byKey: map[string][]string{}, byLoose: map[string][]string{}}
	for _, k := range employerKeys {
		m.byKey[k] = append(m.byKey[k], k)
		if l := LooseKey(k); l != "" {
			m.byLoose[l] = append(m.byLoose[l], k)
		}
	}
	return m
}

// Match returns the employer keys of a company: the same name up to
// legal forms ("Stripe, Inc."), a filing name that adds generic words
// when the name is distinctive enough ("Amazon.com Services LLC"; but
// "Planet Labs" is not "Planet"), and any extra
// filing names configured for it (aliases, matched the same way).
// There is no partial matching, so Compass never claims "Compass Health".
func (m *Matcher) Match(company string, aliases ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ks []string) {
		for _, k := range ks {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	for _, name := range append([]string{company}, aliases...) {
		k := EmployerKey(name)
		add(m.byKey[k])
		if len(k) >= 5 {
			add(m.byLoose[k]) // "Amazon" is "Amazon.com Services LLC"
		}
	}
	return out
}

package resume

import (
	"context"
	"fmt"
	"sort"
)

// ladder runs from the template as written to the tightest layout that is
// still comfortable to read (never below 10pt).
var ladder = []Layout{
	{FontSize: 11, Spacing: 1, Margin: 0.5},
	{FontSize: 11, Spacing: 0.9, Margin: 0.5},
	{FontSize: 10.5, Spacing: 0.9, Margin: 0.45},
	{FontSize: 10.5, Spacing: 0.82, Margin: 0.42},
	{FontSize: 10, Spacing: 0.8, Margin: 0.4},
	{FontSize: 10, Spacing: 0.72, Margin: 0.38},
}

// FitResult explains what one-page fitting did.
type FitResult struct {
	Layout   Layout   `json:"layout"`
	Hidden   []string `json:"hidden"` // bullet ids hidden to fit
	Before   Measure  `json:"before"`
	After    Measure  `json:"after"`
	OnePage  bool     `json:"one_page"`
	Summary  string   `json:"summary"`
	Attempts int      `json:"attempts"`
}

// Priority ranks a bullet for keeping (higher is kept longer). The default
// keeps newer roles and earlier bullets; tailoring adds keyword hits.
type Priority func(sectionIndex, entryIndex, bulletIndex int, s Section, b Bullet) float64

// DefaultPriority keeps experience over projects, recent roles over older
// ones (sections list newest first) and each role's first bullets.
func DefaultPriority(si, ei, bi int, s Section, _ Bullet) float64 {
	weight := 1000.0
	if s.Kind == KindProjects {
		weight = 600
	}
	return weight - float64(ei)*40 - float64(bi)*5 - float64(si)
}

type candidate struct {
	si, ei, bi int
	score      float64
}

// Fit makes the resume one page: the most readable layout that fits, and
// only when none does, the fewest low-priority bullets hidden (never a
// role's first bullet, never something the user chose to show... they can
// unhide anything). It returns the result and changes r (hidden flags).
func (rd *Renderer) Fit(ctx context.Context, r *Resume, prio Priority) (*FitResult, error) {
	if prio == nil {
		prio = DefaultPriority
	}
	res := &FitResult{}
	before, err := rd.Measure(ctx, r, ladder[0])
	if err != nil {
		return nil, err
	}
	res.Before = before
	res.Attempts++

	// 1. The loosest layout that fits as is.
	for _, l := range ladder {
		m := before
		if l != ladder[0] {
			if m, err = rd.Measure(ctx, r, l); err != nil {
				return nil, err
			}
			res.Attempts++
		}
		if m.Pages <= 1 {
			res.Layout, res.After, res.OnePage = l, m, true
			res.Summary = layoutSummary(l)
			return res, nil
		}
	}

	// 2. Hide the lowest-priority bullets at the tightest layout: the
	// fewest that make it fit (binary search over the ranked list).
	var cands []candidate
	for si, s := range r.Sections {
		if s.Hidden || (s.Kind != KindEntries && s.Kind != KindProjects) {
			continue
		}
		for ei, e := range s.Entries {
			if e.Hidden {
				continue
			}
			for bi, b := range e.Bullets {
				if bi == 0 || b.Hidden {
					continue
				}
				cands = append(cands, candidate{si, ei, bi, prio(si, ei, bi, s, b)})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	tight := ladder[len(ladder)-1]
	setHidden := func(k int, on bool) {
		for _, c := range cands[:k] {
			r.Sections[c.si].Entries[c.ei].Bullets[c.bi].Hidden = on
		}
	}
	fits := func(k int, l Layout) (Measure, bool, error) {
		setHidden(len(cands), false)
		setHidden(k, true)
		m, err := rd.Measure(ctx, r, l)
		res.Attempts++
		return m, err == nil && m.Pages <= 1, err
	}
	lo, hi := 1, len(cands)
	if _, ok, err := fits(hi, tight); err != nil {
		return nil, err
	} else if !ok {
		setHidden(len(cands), false)
		m, _ := rd.Measure(ctx, r, tight)
		res.Layout, res.After = tight, m
		res.Summary = fmt.Sprintf("Still %d pages at the tightest readable layout. Remove a role or project, or shorten long bullets.", m.Pages)
		return res, nil
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if _, ok, err := fits(mid, tight); err != nil {
			return nil, err
		} else if ok {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	k := lo
	// 3. With those bullets hidden, prefer the loosest layout that fits.
	for _, l := range ladder {
		if m, ok, err := fits(k, l); err != nil {
			return nil, err
		} else if ok {
			res.Layout, res.After, res.OnePage = l, m, true
			break
		}
	}
	for _, c := range cands[:k] {
		res.Hidden = append(res.Hidden, r.Sections[c.si].Entries[c.ei].Bullets[c.bi].ID)
	}
	res.Summary = fmt.Sprintf("%s, and hid %d lower-priority bullet%s (you can show them again).", layoutSummary(res.Layout), k, plural(k))
	return res, nil
}

func layoutSummary(l Layout) string {
	if l == ladder[0] {
		return "Fits on one page as written"
	}
	return fmt.Sprintf("Fits on one page at %gpt with tighter spacing", l.FontSize)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

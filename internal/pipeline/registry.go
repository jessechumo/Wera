package pipeline

import (
	"wera/internal/config"
	"wera/internal/sources"
	"wera/internal/sources/ashby"
	"wera/internal/sources/greenhouse"
	"wera/internal/sources/lever"
	"wera/internal/sources/workday"
)

// NewSourceRegistry builds the source registry with the shared polite HTTP
// client. It lives here (not in sources) so adapters can import sources
// without a cycle. Adding an ATS means adding a package and one line here
// (the only case that needs code, per PLAN.md section 16).
func NewSourceRegistry(env *config.Env) map[string]sources.Source {
	h := sources.NewHTTP(env.UserAgent)
	return map[string]sources.Source{
		"greenhouse": greenhouse.New(h),
		"lever":      lever.New(h),
		"ashby":      ashby.New(h),
		"workday":    workday.New(h),
	}
}

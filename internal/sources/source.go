// Package sources defines the job source abstraction (Greenhouse, Lever,
// Ashby adapters), the shared polite HTTP client, and the source registry.
package sources

import (
	"context"
	"fmt"
	"time"
)

// RawJob is a posting as fetched from an ATS, before normalization rules
// have been applied (only HTML-to-text has happened, in the adapter).
type RawJob struct {
	ExtID           string
	Title           string
	LocationRaw     string
	URL             string
	Department      string
	DescriptionHTML string
	DescriptionText string // HTML stripped
	IsRemote        *bool
	PostedAt        *time.Time
}

// Source is one ATS adapter. Implementations must be safe for concurrent
// use; one company failing never fails the whole run (callers decide).
type Source interface {
	// Name returns the source identifier, e.g. "greenhouse".
	Name() string
	// Fetch returns all open jobs for the given board token.
	Fetch(ctx context.Context, token string) ([]RawJob, error)
}

// Get returns the source for an ATS name from a registry built by the
// caller (see pipeline.NewSourceRegistry).
func Get(registry map[string]Source, ats string) (Source, error) {
	src, ok := registry[ats]
	if !ok {
		return nil, fmt.Errorf("no source registered for ATS %q", ats)
	}
	return src, nil
}

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

// DetailSource is an ATS whose list endpoint has no job descriptions;
// each posting needs its own request. The fetcher lists the board every
// run but requests details only for postings it has not stored yet, so a
// 2,000-job board costs a few dozen requests per run instead of 2,000.
// Fetch still returns complete jobs (used by `wera discover`).
type DetailSource interface {
	Source
	// List returns the open postings without descriptions.
	List(ctx context.Context, token string) ([]RawJob, error)
	// Detail completes one listed posting (description, dates, ...).
	Detail(ctx context.Context, token string, j *RawJob) error
}

// FetchAll is Fetch for a DetailSource: list, then every detail.
func FetchAll(ctx context.Context, s DetailSource, token string) ([]RawJob, error) {
	jobs, err := s.List(ctx, token)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		if err := s.Detail(ctx, token, &jobs[i]); err != nil {
			return nil, err
		}
	}
	return jobs, nil
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

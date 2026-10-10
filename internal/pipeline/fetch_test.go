package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"wera/internal/config"
	"wera/internal/sources"
	"wera/internal/testutil"
)

// etagBoard is a ConditionalSource whose postings the test can change; it
// answers 304 while its version matches the caller's ETag.
type etagBoard struct {
	mu      sync.Mutex
	version string
	jobs    []sources.RawJob
	calls   atomic.Int64
}

func (b *etagBoard) Name() string { return "fakeboard" }
func (b *etagBoard) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	jobs, _, err := b.FetchIfChanged(ctx, token, "")
	return jobs, err
}
func (b *etagBoard) FetchIfChanged(_ context.Context, _, etag string) ([]sources.RawJob, string, error) {
	b.calls.Add(1)
	b.mu.Lock()
	defer b.mu.Unlock()
	if etag != "" && etag == b.version {
		return nil, "", sources.ErrNotModified
	}
	return append([]sources.RawJob(nil), b.jobs...), b.version, nil
}

func (b *etagBoard) set(version string, jobs ...sources.RawJob) {
	b.mu.Lock()
	b.version, b.jobs = version, jobs
	b.mu.Unlock()
}

func raw(id, title string) sources.RawJob {
	return sources.RawJob{ExtID: id, Title: title, LocationRaw: "Remote, US", URL: "https://example.com/" + id,
		DescriptionText: "About the role: " + title}
}

func newFetcher(t *testing.T, srcs map[string]sources.Source) *Fetcher {
	return &Fetcher{
		Pool:    testutil.Pool(t),
		Env:     &config.Env{FetchConcurrency: 2},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sources: srcs,
	}
}

func TestFetchConditionalBoard(t *testing.T) {
	board := &etagBoard{}
	f := newFetcher(t, map[string]sources.Source{"fakeboard": board})
	ctx := context.Background()
	_, name := testutil.Company(t, f.Pool, "ai_ml")
	cos := []config.Company{{Name: name, ATS: "fakeboard", Token: name, Industry: "ai_ml"}}

	board.set("v1", raw("a", "Site Reliability Engineer"), raw("b", "Platform Engineer"))
	st, err := f.Run(ctx, cos, name)
	if err != nil || st.CompaniesOK != 1 || st.JobsNew != 2 {
		t.Fatalf("first fetch: %+v %v", st, err)
	}

	// Same version: a 304, nothing written.
	st, _ = f.Run(ctx, cos, name)
	if st.CompaniesUnchanged != 1 || st.JobsNew != 0 {
		t.Errorf("unchanged board: %+v", st)
	}

	// "b" closes, "a" changes, "c" is new.
	changed := raw("a", "Site Reliability Engineer II")
	board.set("v2", changed, raw("c", "DevOps Engineer"))
	st, _ = f.Run(ctx, cos, name)
	if st.JobsNew != 1 || st.CompaniesUnchanged != 0 {
		t.Errorf("changed board: %+v", st)
	}
	var open, closed int
	f.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE closed_at IS NULL), count(*) FILTER (WHERE closed_at IS NOT NULL)
		FROM jobs j JOIN companies c ON c.id = j.company_id WHERE c.name = $1`, name).Scan(&open, &closed)
	if open != 2 || closed != 1 {
		t.Errorf("want 2 open and 1 closed, got %d and %d", open, closed)
	}
	var title string
	f.Pool.QueryRow(ctx, `SELECT title FROM jobs j JOIN companies c ON c.id = j.company_id
		WHERE c.name = $1 AND ext_id = 'a'`, name).Scan(&title)
	if title != changed.Title {
		t.Errorf("changed posting not updated: %q", title)
	}
}

// failingBoard fails every fetch with err.
type failingBoard struct{ err error }

func (b failingBoard) Name() string { return "failboard" }
func (b failingBoard) Fetch(context.Context, string) ([]sources.RawJob, error) {
	return nil, b.err
}

func TestFetchFailuresDoNotFailTheRun(t *testing.T) {
	f := newFetcher(t, map[string]sources.Source{
		"down":  failingBoard{errors.New("boom")},
		"maint": failingBoard{sources.ErrUnavailable},
	})
	ctx := context.Background()
	_, broken := testutil.Company(t, f.Pool, "ai_ml")
	_, maint := testutil.Company(t, f.Pool, "ai_ml")
	st, err := f.Run(ctx, []config.Company{
		{Name: broken, ATS: "down", Token: broken, Industry: "ai_ml"},
		{Name: maint, ATS: "maint", Token: maint, Industry: "ai_ml"},
	}, "")
	if err != nil {
		t.Fatalf("a failing board failed the run: %v", err)
	}
	if st.CompaniesFailed != 1 || st.CompaniesDeferred != 1 || st.RetryAt.IsZero() {
		t.Errorf("stats: %+v", st)
	}
	var ok *bool
	var msg *string
	f.Pool.QueryRow(ctx, `SELECT last_fetch_ok, last_fetch_error FROM companies WHERE name = $1`, broken).Scan(&ok, &msg)
	if ok == nil || *ok || msg == nil || *msg != "boom" {
		t.Errorf("failure not recorded: %v %v", ok, msg)
	}
	if deferredOf(st) != 1 {
		t.Errorf("deferredOf: %d", deferredOf(st))
	}
}

func TestFetchUnknownCompany(t *testing.T) {
	f := newFetcher(t, nil)
	if _, err := f.Run(context.Background(), nil, "no-such-company"); err == nil {
		t.Error("want an error for an unknown company")
	}
}

// detailBoard is a DetailSource: the list has no descriptions and every
// detail is its own request (counted).
type detailBoard struct {
	jobs    []sources.RawJob
	details atomic.Int64
}

func (b *detailBoard) Name() string { return "detailboard" }
func (b *detailBoard) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	return sources.FetchAll(ctx, b, token)
}
func (b *detailBoard) List(context.Context, string) ([]sources.RawJob, error) {
	out := make([]sources.RawJob, len(b.jobs))
	for i, j := range b.jobs {
		out[i] = sources.RawJob{ExtID: j.ExtID, Title: j.Title, LocationRaw: j.LocationRaw, URL: j.URL}
	}
	return out, nil
}
func (b *detailBoard) Detail(_ context.Context, _ string, j *sources.RawJob) error {
	b.details.Add(1)
	j.DescriptionText = "Details for " + j.Title
	return nil
}

func TestFetchDetailsOnlyForNewPostings(t *testing.T) {
	board := &detailBoard{jobs: []sources.RawJob{raw("1", "SRE"), raw("2", "Platform Engineer")}}
	f := newFetcher(t, map[string]sources.Source{"detailboard": board})
	ctx := context.Background()
	_, name := testutil.Company(t, f.Pool, "ai_ml")
	cos := []config.Company{{Name: name, ATS: "detailboard", Token: name, Industry: "ai_ml"}}

	if st, err := f.Run(ctx, cos, name); err != nil || st.JobsNew != 2 {
		t.Fatalf("first fetch: %+v %v", st, err)
	}
	if board.details.Load() != 2 {
		t.Fatalf("first fetch details: %d", board.details.Load())
	}
	board.jobs = append(board.jobs, raw("3", "DevOps Engineer"))
	if st, _ := f.Run(ctx, cos, name); st.JobsNew != 1 {
		t.Errorf("second fetch: %+v", st)
	}
	if board.details.Load() != 3 {
		t.Errorf("known postings were fetched again: %d detail requests", board.details.Load())
	}
}

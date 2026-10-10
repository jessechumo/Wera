package scoring

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// FactsResult is one facts extraction.
type FactsResult struct {
	JobID     int64
	Facts     *Facts // nil when the call failed or the reply was unusable
	Raw       string
	Usage     UsageStats
	CostUSD   float64
	LatencyMS int64
	Err       error
}

// Extractor pulls shared facts from postings with bounded concurrency and
// a cost budget. Every call shares FactsCacheKey, so the system prompt is
// a cached read for all postings and all users.
type Extractor struct {
	Client      *Client
	Model       string
	Concurrency int
	MaxCostUSD  float64 // 0 disables the guard
	Log         *slog.Logger
	// OnResult, when set, receives each result as soon as it is ready.
	OnResult func(FactsResult)

	mu    sync.Mutex
	spent float64
}

// Extract runs every job in order until the budget runs out; jobs not
// attempted are reported with a nil Facts and no error.
func (e *Extractor) Extract(ctx context.Context, jobs []Job) []FactsResult {
	if e.Concurrency < 1 {
		e.Concurrency = 1
	}
	sem := make(chan struct{}, e.Concurrency)
	out := make([]FactsResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		if e.over() || ctx.Err() != nil {
			for k := i; k < len(jobs); k++ {
				out[k] = FactsResult{JobID: jobs[k].ID}
			}
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, j Job) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = e.one(ctx, j)
			if e.OnResult != nil {
				e.OnResult(out[i])
			}
		}(i, j)
	}
	wg.Wait()
	return out
}

func (e *Extractor) one(ctx context.Context, j Job) FactsResult {
	r := FactsResult{JobID: j.ID}
	start := time.Now()
	comp, err := e.Client.Chat(ctx, FactsCacheKey, BuildFactsMessages(j))
	if err != nil {
		r.Err = err
		return r
	}
	r.Usage, r.LatencyMS, r.Raw = comp.Usage, time.Since(start).Milliseconds(), StripFences(comp.Content)
	if comp.CostUSD != nil && *comp.CostUSD > 0 {
		r.CostUSD = *comp.CostUSD
	} else {
		p, _ := PriceFor(e.Model)
		r.CostUSD = p.CostUSD(comp.Usage.PromptTokens, comp.Usage.CachedTokens, comp.Usage.CompletionTokens)
	}
	e.mu.Lock()
	e.spent += r.CostUSD
	e.mu.Unlock()
	r.Facts, r.Err = ParseFacts(comp.Content, j.Description)
	return r
}

func (e *Extractor) over() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.MaxCostUSD > 0 && e.spent >= e.MaxCostUSD
}

// Spent is the cost of the calls made so far.
func (e *Extractor) Spent() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spent
}

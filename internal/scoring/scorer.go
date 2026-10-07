package scoring

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Job stages set by the scorer.
const (
	StageScored   = "scored"
	StageExcluded = "excluded"
	StageFailed   = "score_failed"
	StageSkipped  = "" // budget reached or call error; job stays pending_score
)

// Outcome is the result of scoring one job.
type Outcome struct {
	JobID           int64
	Stage           string // scored | excluded | score_failed | "" (skipped)
	ExcludeReason   string
	ExcludeEvidence string
	Analysis        *Analysis
	Raw             string // raw model text (fence-stripped)
	Usage           UsageStats
	CostUSD         float64
	LatencyMS       int64
	Err             error
}

// retryNote is the extra message appended when the model returns invalid
// JSON, per PLAN.md section 7.4.
const retryNote = "Your previous reply was not valid JSON for the schema. Return only the JSON object."

// Scorer runs the scoring stage over a batch of pending jobs with
// bounded concurrency, a per-run cost budget, and post-LLM exclusions.
type Scorer struct {
	Client      *Client
	Profile     []byte // profile.md, verbatim
	ProfileHash string
	Model       string
	MaxYears    int     // roles.yaml seniority.max_years_required
	MaxCostUSD  float64 // budget guard; 0 disables
	Concurrency int     // SCORING_CONCURRENCY
	Log         *slog.Logger

	// Mutable run state.
	limit     atomic.Int32 // current effective concurrency
	inUse     atomic.Int32
	costMu    sync.Mutex
	spent     float64
	budgetHit bool
}

// Score scores every job with up to Concurrency parallel calls. Jobs left
// unscored because the budget was reached have Stage StageSkipped and
// stay pending for the next run. The returned slice is in input order.
func (s *Scorer) Score(ctx context.Context, jobs []Job) []Outcome {
	if s.Concurrency < 1 {
		s.Concurrency = 1
	}
	s.limit.Store(int32(s.Concurrency))
	if s.Client != nil {
		s.Client.OnRateLimit = s.halveConcurrency
	}

	outcomes := make([]Outcome, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i] = s.scoreOne(ctx, jobs[i])
		}(i)
	}
	wg.Wait()
	return outcomes
}

// scoreOne scores a single job end to end.
func (s *Scorer) scoreOne(ctx context.Context, j Job) Outcome {
	out := Outcome{JobID: j.ID}

	if !s.reserve(ctx) {
		out.Stage = StageSkipped
		return out
	}
	defer s.release()

	messages := BuildMessages(s.Profile, j)
	start := time.Now()
	comp, err := s.Client.Chat(ctx, CacheKey(s.ProfileHash), messages)
	if err != nil {
		// A transport/API error leaves the job pending for the next run.
		out.Stage = StageSkipped
		out.Err = err
		if s.Log != nil {
			s.Log.Warn("scoring call failed; job stays pending", "job_id", j.ID, "err", err)
		}
		return out
	}
	out.Usage = comp.Usage
	out.LatencyMS = time.Since(start).Milliseconds()
	if comp.CostUSD != nil && *comp.CostUSD > 0 {
		out.CostUSD = *comp.CostUSD // authoritative, from the provider
	} else {
		prices, _ := PriceFor(s.Model)
		out.CostUSD = prices.CostUSD(comp.Usage.PromptTokens, comp.Usage.CachedTokens, comp.Usage.CompletionTokens)
	}
	s.addCost(out.CostUSD)

	analysis, raw := s.parseWithRetry(ctx, messages, comp.Content)
	out.Raw = raw
	if analysis == nil {
		out.Stage = StageFailed // raw text is saved with the analysis row
		return out
	}
	out.Analysis = analysis

	reason, evidence := PostLLMExclusion(analysis, s.MaxYears)
	if reason != "" {
		out.Stage = StageExcluded
		out.ExcludeReason = reason
		out.ExcludeEvidence = evidence
	} else {
		out.Stage = StageScored
	}
	return out
}

// parseWithRetry parses the reply; on a schema failure it retries once
// with the retry note appended. It returns the analysis (nil on final
// failure) and the raw text to persist.
func (s *Scorer) parseWithRetry(ctx context.Context, messages []Message, content string) (*Analysis, string) {
	a, err := ParseAnalysis(content)
	if err == nil {
		return a, StripFences(content)
	}
	if s.Log != nil {
		s.Log.Warn("invalid scoring JSON, retrying once", "err", err)
	}
	comp, cerr := s.Client.Chat(ctx, CacheKey(s.ProfileHash),
		append(append([]Message{}, messages...), Message{Role: "user", Content: retryNote}))
	if cerr != nil {
		return nil, content
	}
	a, err = ParseAnalysis(comp.Content)
	if err != nil {
		return nil, comp.Content
	}
	return a, StripFences(comp.Content)
}

// PostLLMExclusion applies the post-LLM exclusion rules from PLAN.md
// section 7.4, returning the exclude reason and evidence, or "" when the
// job should stay scored. Rules in order: sponsorship refusal, years
// ceiling, US ineligibility, senior seniority.
func PostLLMExclusion(a *Analysis, maxYears int) (reason, evidence string) {
	switch {
	case a.Sponsorship == "no" && a.SponsorshipQuote != nil && *a.SponsorshipQuote != "":
		return "llm:sponsorship_no", *a.SponsorshipQuote
	case a.YearsRequired != nil && *a.YearsRequired > maxYears:
		return "llm:years>" + strconv.Itoa(maxYears), a.Reason
	case a.USEligible != nil && !*a.USEligible:
		return "llm:non_us", a.LocationSummary
	case a.Seniority == "senior":
		return "llm:senior", a.Reason
	}
	return "", ""
}

// reserve takes a concurrency slot, respecting the (possibly halved)
// dynamic limit and the budget. It returns false when the run must stop.
func (s *Scorer) reserve(ctx context.Context) bool {
	for {
		if s.budgetReached() {
			return false
		}
		lim := s.limit.Load()
		use := s.inUse.Load()
		if use < lim && s.inUse.CompareAndSwap(use, use+1) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// release returns a concurrency slot.
func (s *Scorer) release() { s.inUse.Add(-1) }

// halveConcurrency drops the effective concurrency for the rest of the
// run after a 429, never below 1.
func (s *Scorer) halveConcurrency() {
	for {
		cur := s.limit.Load()
		next := cur / 2
		if next < 1 {
			next = 1
		}
		if s.limit.CompareAndSwap(cur, next) {
			if s.Log != nil {
				s.Log.Warn("429 seen; halving scoring concurrency", "new_limit", next)
			}
			return
		}
	}
}

// addCost accumulates spend under the budget guard.
func (s *Scorer) addCost(c float64) {
	s.costMu.Lock()
	defer s.costMu.Unlock()
	s.spent += c
	if s.MaxCostUSD > 0 && s.spent >= s.MaxCostUSD {
		s.budgetHit = true
	}
}

// budgetReached reports whether the per-run cost budget is exhausted.
func (s *Scorer) budgetReached() bool {
	s.costMu.Lock()
	defer s.costMu.Unlock()
	return s.budgetHit
}

// Spent returns the accumulated cost of this scoring run.
func (s *Scorer) Spent() float64 {
	s.costMu.Lock()
	defer s.costMu.Unlock()
	return s.spent
}

// ErrNoProfile is returned when the profile file is missing or empty.
var ErrNoProfile = errors.New("candidate profile is missing or empty")

// Package scoring implements the LLM scoring stage: a client for any
// OpenAI-compatible chat-completions API (Coral Bricks, DeepInfra, ...),
// prompt construction with a byte-identical cached prefix, JSON schema
// validation, cost accounting, post-LLM exclusions, and the concurrent
// scorer (PLAN.md section 7.4).
package scoring

import "sync"

// Prices are USD per 1M tokens for one model.
type Prices struct {
	Input       float64 // uncached prompt tokens
	CachedInput float64 // prompt tokens read from the provider's cache (0 = free)
	CacheWrite  float64 // first-time prefix write (informational; providers that bill it report their own cost)
	Output      float64 // completion tokens
}

// priceTable lists the known models by the name each provider uses.
// Providers that report a per-call cost (Coral, DeepInfra) are billed by
// that figure; the table covers the rest and sanity-checks budgets. Add a
// model with AI_PRICE_* (see config) instead of editing this.
var (
	priceMu    sync.RWMutex
	priceTable = map[string]Prices{
		// Coral Bricks (cached reads are free).
		"glm-5.3-flash-fast":       {Input: 0.15, CacheWrite: 0.23, Output: 0.50},
		"glm-5.3-fast":             {Input: 1.12, CacheWrite: 1.68, Output: 4.40},
		"deepseek-v4.1-flash-fast": {Input: 0.30, CacheWrite: 0.09, Output: 1.20},
		// DeepInfra (from its /models metadata, October 2026).
		"deepseek-ai/DeepSeek-V4.1-Flash": {Input: 0.20, CachedInput: 0.006, Output: 0.60},
		"deepseek-ai/DeepSeek-V4-Flash":   {Input: 0.09, CachedInput: 0.018, Output: 0.18},
		"zai-org/GLM-5.3":                 {Input: 0.90, CachedInput: 0.20, Output: 4.00},
		"zai-org/GLM-5.3-Flash":           {Input: 0.075, CachedInput: 0.015, Output: 0.25},
	}
)

// PriceFor returns the price table entry for a model and whether it exists.
func PriceFor(model string) (Prices, bool) {
	priceMu.RLock()
	defer priceMu.RUnlock()
	p, ok := priceTable[model]
	return p, ok
}

// SetPrice adds or replaces a model's prices (from configuration).
func SetPrice(model string, p Prices) {
	priceMu.Lock()
	defer priceMu.Unlock()
	priceTable[model] = p
}

// CostUSD computes the cost of one call: uncached input at the input
// price, cached input at the cached price, completion at the output price.
func (p Prices) CostUSD(promptTokens, cachedTokens, completionTokens int64) float64 {
	cachedTokens = max(0, min(cachedTokens, promptTokens))
	uncached := max(0, promptTokens-cachedTokens)
	completionTokens = max(0, completionTokens)
	return (float64(uncached)*p.Input + float64(cachedTokens)*p.CachedInput + float64(completionTokens)*p.Output) / 1e6
}

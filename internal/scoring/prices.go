// Package scoring implements the LLM scoring stage: a Coral Bricks
// chat-completions client, prompt construction with a byte-identical
// cached prefix, JSON schema validation, cost accounting, post-LLM
// exclusions, and the concurrent scorer (PLAN.md section 7.4).
package scoring

// Prices are USD per 1M tokens, keyed by model name (PLAN.md section 5.1).
// Cached input reads are free; the uncached input portion is charged at
// the input price.
type Prices struct {
	Input      float64 // per 1M tokens
	CacheWrite float64 // per 1M tokens (first-time prefix write)
	Output     float64 // per 1M tokens
}

// priceTable lists the known models. An unknown model scores with zero
// cost rather than failing the run.
var priceTable = map[string]Prices{
	"glm-5.3-flash-fast":       {Input: 0.15, CacheWrite: 0.23, Output: 0.50},
	"glm-5.3-fast":             {Input: 1.12, CacheWrite: 1.68, Output: 4.40},
	"deepseek-v4.1-flash-fast": {Input: 0.30, CacheWrite: 0.09, Output: 1.20},
}

// PriceFor returns the price table entry for a model and whether it exists.
func PriceFor(model string) (Prices, bool) {
	p, ok := priceTable[model]
	return p, ok
}

// CostUSD computes the cost of one call: (uncached input x input price +
// completion x output price) / 1e6. Cached prompt tokens are free reads.
func (p Prices) CostUSD(promptTokens, cachedTokens, completionTokens int64) float64 {
	uncached := promptTokens - cachedTokens
	if uncached < 0 {
		uncached = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	return (float64(uncached)*p.Input + float64(completionTokens)*p.Output) / 1e6
}

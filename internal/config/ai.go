package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wera/internal/scoring"
)

// AI is the language-model provider every AI feature uses (scoring, job
// facts, resumes, cover letters, moderation). Any OpenAI-compatible
// chat-completions API works; AI_PROVIDER picks a preset and AI_* settings
// override it:
//
//	AI_PROVIDER=deepinfra
//	AI_API_KEY=...
//	AI_MODEL=deepseek-ai/DeepSeek-V4.1-Flash   # optional, the preset's default
type AI struct {
	Provider  string // coral | deepinfra | openai (any other OpenAI-compatible API)
	APIKey    string
	BaseURL   string
	Model     string         // scoring, facts and every other call
	DeepModel string         // deep review and other heavier calls
	CacheKeys bool           // send prompt_cache_key (providers that route by it)
	ExtraBody map[string]any // merged into every request (AI_EXTRA_BODY, JSON)
}

// Enabled reports whether an API key is set (AI features are off without).
func (a AI) Enabled() bool { return a.APIKey != "" }

// Options are the client options for one model on this provider; callers
// add timeouts, token limits and logging.
func (a AI) Options(model string) scoring.ClientOptions {
	return scoring.ClientOptions{BaseURL: a.BaseURL, APIKey: a.APIKey, Model: model,
		CacheKeys: a.CacheKeys, ExtraBody: a.ExtraBody}
}

type aiPreset struct {
	baseURL, model, deepModel string
	cacheKeys                 bool
}

var aiPresets = map[string]aiPreset{
	"coral":     {"https://inference.coralbricks.ai/v1", "deepseek-v4.1-flash-fast", "glm-5.3-fast", true},
	"deepinfra": {"https://api.deepinfra.com/v1/openai", "deepseek-ai/DeepSeek-V4.1-Flash", "zai-org/GLM-5.3", false},
	"openai":    {"", "", "", false}, // AI_BASE_URL and AI_MODEL are required
}

// loadAI reads AI_* settings. The older CORAL_* names still work when the
// provider is Coral (the default), so existing .env files need no change.
func loadAI() (AI, []string) {
	var errs []string
	get := func(key, legacy string, coral bool) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if coral && legacy != "" {
			return os.Getenv(legacy)
		}
		return ""
	}

	a := AI{Provider: strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER")))}
	if a.Provider == "" {
		a.Provider = "coral"
	}
	preset, ok := aiPresets[a.Provider]
	if !ok {
		return a, []string{fmt.Sprintf("AI_PROVIDER must be coral, deepinfra or openai, got %q", a.Provider)}
	}
	coral := a.Provider == "coral"
	a.BaseURL, a.Model, a.DeepModel, a.CacheKeys = preset.baseURL, preset.model, preset.deepModel, preset.cacheKeys
	if v := get("AI_API_KEY", "CORAL_API_KEY", coral); v != "" {
		a.APIKey = v
	}
	if v := get("AI_BASE_URL", "CORAL_BASE_URL", coral); v != "" {
		a.BaseURL = strings.TrimRight(v, "/")
	}
	if v := get("AI_MODEL", "CORAL_MODEL", coral); v != "" {
		a.Model = v
	}
	if v := get("AI_DEEP_MODEL", "CORAL_DEEP_MODEL", coral); v != "" {
		a.DeepModel = v
	}
	if a.DeepModel == "" {
		a.DeepModel = a.Model
	}
	if v := os.Getenv("AI_PROMPT_CACHE_KEY"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("AI_PROMPT_CACHE_KEY: not a boolean: %q", v))
		}
		a.CacheKeys = b
	}
	if v := os.Getenv("AI_EXTRA_BODY"); v != "" {
		if err := json.Unmarshal([]byte(v), &a.ExtraBody); err != nil {
			errs = append(errs, "AI_EXTRA_BODY must be a JSON object: "+err.Error())
		}
	}

	// Prices for a model the table does not know (USD per 1M tokens).
	var p scoring.Prices
	priced := false
	for key, dst := range map[string]*float64{
		"AI_PRICE_INPUT": &p.Input, "AI_PRICE_CACHED_INPUT": &p.CachedInput, "AI_PRICE_OUTPUT": &p.Output,
	} {
		if v := os.Getenv(key); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 {
				errs = append(errs, fmt.Sprintf("%s: not a price: %q", key, v))
				continue
			}
			*dst, priced = f, true
		}
	}
	if priced && a.Model != "" {
		scoring.SetPrice(a.Model, p)
	}

	switch {
	case a.BaseURL == "" || a.Model == "":
		errs = append(errs, fmt.Sprintf("AI_PROVIDER=%s needs AI_BASE_URL and AI_MODEL", a.Provider))
	case !secureURL(a.BaseURL):
		errs = append(errs, fmt.Sprintf("AI_BASE_URL must use https (or http to localhost), got %q", a.BaseURL))
	}
	// Budgets need a price for every model that is called. Providers that
	// report costs are billed by their figure, but the table backs it up.
	if a.Enabled() {
		for _, m := range []string{a.Model, a.DeepModel} {
			if _, ok := scoring.PriceFor(m); !ok && m != "" {
				errs = append(errs, fmt.Sprintf("no known price for model %q: set AI_PRICE_INPUT, AI_PRICE_CACHED_INPUT and AI_PRICE_OUTPUT (USD per 1M tokens)", m))
			}
		}
	}
	return a, errs
}

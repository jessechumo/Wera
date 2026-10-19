package config

import (
	"strings"
	"testing"

	"wera/internal/scoring"
)

// clearAI unsets every AI setting so the process environment cannot leak in.
func clearAI(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AI_PROVIDER", "AI_API_KEY", "AI_BASE_URL", "AI_MODEL", "AI_DEEP_MODEL",
		"AI_PROMPT_CACHE_KEY", "AI_EXTRA_BODY", "AI_PRICE_INPUT", "AI_PRICE_CACHED_INPUT", "AI_PRICE_OUTPUT",
		"CORAL_API_KEY", "CORAL_BASE_URL", "CORAL_MODEL", "CORAL_DEEP_MODEL"} {
		t.Setenv(k, "")
	}
}

func TestAIDefaultsToCoralWithLegacyNames(t *testing.T) {
	clearAI(t)
	t.Setenv("CORAL_API_KEY", "cb_legacy")
	t.Setenv("CORAL_MODEL", "glm-5.3-flash-fast")
	a, errs := loadAI()
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if a.Provider != "coral" || a.APIKey != "cb_legacy" || a.Model != "glm-5.3-flash-fast" ||
		a.DeepModel != "glm-5.3-fast" || !a.CacheKeys || !strings.Contains(a.BaseURL, "coralbricks") {
		t.Errorf("coral defaults: %+v", a)
	}
}

func TestAIDeepInfraPreset(t *testing.T) {
	clearAI(t)
	t.Setenv("AI_PROVIDER", "DeepInfra")
	t.Setenv("AI_API_KEY", "di_key")
	t.Setenv("CORAL_MODEL", "deepseek-v4.1-flash-fast") // left over in an old .env: ignored
	t.Setenv("AI_EXTRA_BODY", `{"reasoning_effort":"none"}`)
	a, errs := loadAI()
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if a.BaseURL != "https://api.deepinfra.com/v1/openai" || a.Model != "deepseek-ai/DeepSeek-V4.1-Flash" ||
		a.DeepModel != "zai-org/GLM-5.3" || a.CacheKeys || a.ExtraBody["reasoning_effort"] != "none" {
		t.Errorf("deepinfra preset: %+v", a)
	}
	o := a.Options(a.Model)
	if o.APIKey != "di_key" || o.Model != a.Model || o.CacheKeys || o.ExtraBody["reasoning_effort"] != "none" {
		t.Errorf("client options: %+v", o)
	}
}

func TestAIOpenAICompatibleNeedsURLModelAndPrice(t *testing.T) {
	clearAI(t)
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("AI_API_KEY", "k")
	if _, errs := loadAI(); len(errs) == 0 || !strings.Contains(strings.Join(errs, " "), "AI_BASE_URL and AI_MODEL") {
		t.Errorf("missing URL/model accepted: %v", errs)
	}

	t.Setenv("AI_BASE_URL", "https://llm.example.com/v1/")
	t.Setenv("AI_MODEL", "acme/unpriced-model")
	if _, errs := loadAI(); len(errs) == 0 || !strings.Contains(strings.Join(errs, " "), "no known price") {
		t.Errorf("unpriced model accepted: %v", errs)
	}

	t.Setenv("AI_PRICE_INPUT", "0.5")
	t.Setenv("AI_PRICE_CACHED_INPUT", "0.05")
	t.Setenv("AI_PRICE_OUTPUT", "1.5")
	a, errs := loadAI()
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if a.BaseURL != "https://llm.example.com/v1" || a.DeepModel != "acme/unpriced-model" {
		t.Errorf("openai-compatible: %+v", a)
	}
	if p, ok := scoring.PriceFor("acme/unpriced-model"); !ok || p.Output != 1.5 || p.CachedInput != 0.05 {
		t.Errorf("price override: %+v %v", p, ok)
	}
}

func TestAIRejectsBadSettings(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"unknown provider": {"AI_PROVIDER": "acme"},
		"plain http":       {"AI_BASE_URL": "http://llm.example.com/v1"},
		"bad extra body":   {"AI_EXTRA_BODY": "not json"},
		"bad price":        {"AI_PRICE_OUTPUT": "-1"},
		"bad cache flag":   {"AI_PROMPT_CACHE_KEY": "maybe"},
	} {
		t.Run(name, func(t *testing.T) {
			clearAI(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, errs := loadAI(); len(errs) == 0 {
				t.Error("accepted")
			}
		})
	}
}

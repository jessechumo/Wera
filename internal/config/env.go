package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"wera/internal/buildinfo"
)

// Env holds environment-derived configuration with the defaults from
// PLAN.md section 5.1. Real environment variables (or a local .env, loaded
// by the main package via godotenv) override every default.
type Env struct {
	DatabaseURL        string
	CoralAPIKey        string
	CoralBaseURL       string
	CoralModel         string
	CoralDeepModel     string
	ScoringConcurrency int
	FetchConcurrency   int
	RunInterval        time.Duration
	RunSchedule        *Schedule // nil: loop every RunInterval
	HTTPAddr           string
	LogFormat          string
	UserAgent          string
	MaxCostPerRunUSD   float64 // per user per run
	UserBudgetUSD      float64 // monthly budget given to new accounts
	MaxMonthlyCostUSD  float64 // all users together, per calendar month

	// Accounts and browser security.
	SignupEnabled  bool     // POST /api/auth/signup open to anyone
	SignupsPerHour int      // sign-ups allowed per client IP per hour
	CookieSecure   bool     // set Secure on the session cookie (HTTPS only)
	PublicOrigins  []string // extra origins allowed to send state-changing requests
	TrustProxy     bool     // take the client IP from proxy headers
}

// LoadEnv builds an Env from the process environment with defaults.
func LoadEnv() (*Env, error) {
	e := &Env{ //nolint:gosec // DatabaseURL is a local development default; deployments set DATABASE_URL
		DatabaseURL:        "postgres://wera:wera@localhost:5433/wera?sslmode=disable",
		CoralBaseURL:       "https://inference.coralbricks.ai/v1",
		CoralModel:         "deepseek-v4.1-flash-fast",
		CoralDeepModel:     "glm-5.3-fast",
		ScoringConcurrency: 12,
		FetchConcurrency:   6,
		RunInterval:        30 * time.Minute,
		HTTPAddr:           "127.0.0.1:8080",
		LogFormat:          "text",
		UserAgent:          "Wera/" + strings.TrimPrefix(buildinfo.Version, "v") + " (personal job tracker; contact: jessechumo@gmail.com)",
		MaxCostPerRunUSD:   1.00,
		UserBudgetUSD:      10,
		MaxMonthlyCostUSD:  100,
		SignupEnabled:      true,
		SignupsPerHour:     5,
	}

	var errs []string
	str := func(key string, dst *string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	intVal := func(key string, dst *int) {
		if v := os.Getenv(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: not an integer: %q", key, v))
				return
			}
			*dst = n
		}
	}
	durVal := func(key string, dst *time.Duration) {
		if v := os.Getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: not a duration: %q", key, v))
				return
			}
			*dst = d
		}
	}
	boolVal := func(key string, dst *bool) {
		if v := os.Getenv(key); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: not a boolean: %q", key, v))
				return
			}
			*dst = b
		}
	}
	floatVal := func(key string, dst *float64) {
		if v := os.Getenv(key); v != "" {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: not a number: %q", key, v))
				return
			}
			*dst = f
		}
	}

	str("DATABASE_URL", &e.DatabaseURL)
	str("CORAL_API_KEY", &e.CoralAPIKey)
	str("CORAL_BASE_URL", &e.CoralBaseURL)
	str("CORAL_MODEL", &e.CoralModel)
	str("CORAL_DEEP_MODEL", &e.CoralDeepModel)
	intVal("SCORING_CONCURRENCY", &e.ScoringConcurrency)
	intVal("FETCH_CONCURRENCY", &e.FetchConcurrency)
	durVal("RUN_INTERVAL", &e.RunInterval)
	tzName := "America/Chicago"
	str("RUN_TIMEZONE", &tzName)
	if loc, err := time.LoadLocation(tzName); err != nil {
		errs = append(errs, fmt.Sprintf("RUN_TIMEZONE: unknown time zone %q", tzName))
	} else if sched, err := ParseSchedule(os.Getenv("RUN_SCHEDULE"), loc); err != nil {
		errs = append(errs, "RUN_SCHEDULE: "+err.Error())
	} else {
		e.RunSchedule = sched
	}
	str("HTTP_ADDR", &e.HTTPAddr)
	str("LOG_FORMAT", &e.LogFormat)
	str("USER_AGENT", &e.UserAgent)
	floatVal("MAX_COST_PER_RUN_USD", &e.MaxCostPerRunUSD)
	floatVal("USER_MONTHLY_BUDGET_USD", &e.UserBudgetUSD)
	floatVal("MAX_MONTHLY_COST_USD", &e.MaxMonthlyCostUSD)
	boolVal("SIGNUP_ENABLED", &e.SignupEnabled)
	intVal("SIGNUPS_PER_HOUR", &e.SignupsPerHour)
	boolVal("COOKIE_SECURE", &e.CookieSecure)
	boolVal("TRUST_PROXY", &e.TrustProxy)
	for _, o := range strings.Split(os.Getenv("PUBLIC_ORIGINS"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			e.PublicOrigins = append(e.PublicOrigins, o)
		}
	}

	if e.FetchConcurrency < 1 {
		errs = append(errs, "FETCH_CONCURRENCY must be >= 1")
	}
	if e.ScoringConcurrency < 1 {
		errs = append(errs, "SCORING_CONCURRENCY must be >= 1")
	}
	if e.RunInterval <= 0 {
		errs = append(errs, "RUN_INTERVAL must be positive")
	}
	if e.MaxCostPerRunUSD <= 0 {
		errs = append(errs, "MAX_COST_PER_RUN_USD must be positive")
	}
	if e.UserBudgetUSD < 0 || e.MaxMonthlyCostUSD <= 0 {
		errs = append(errs, "USER_MONTHLY_BUDGET_USD must be >= 0 and MAX_MONTHLY_COST_USD positive")
	}
	if e.LogFormat != "text" && e.LogFormat != "json" {
		errs = append(errs, fmt.Sprintf("LOG_FORMAT must be text or json, got %q", e.LogFormat))
	}
	if !secureURL(e.CoralBaseURL) {
		errs = append(errs, fmt.Sprintf("CORAL_BASE_URL must use https (or http to localhost), got %q", e.CoralBaseURL))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid environment:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return e, nil
}

// NewLogger returns a slog logger using the configured format
// (text locally, JSON on the server).
func NewLogger(e *Env) *slog.Logger {
	if e.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

// secureURL accepts https URLs, and plain http only to this machine
// (a local mock). The API key and resumes must never cross a network in
// the clear.
func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}

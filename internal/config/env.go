package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
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
	HTTPAddr           string
	LogFormat          string
	UserAgent          string
	MaxCostPerRunUSD   float64
}

// LoadEnv builds an Env from the process environment with defaults.
func LoadEnv() (*Env, error) {
	e := &Env{
		DatabaseURL:        "postgres://wera:wera@localhost:5433/wera?sslmode=disable",
		CoralBaseURL:       "https://inference.coralbricks.ai/v1",
		CoralModel:         "glm-5.3-flash-fast",
		CoralDeepModel:     "glm-5.3-fast",
		ScoringConcurrency: 12,
		FetchConcurrency:   6,
		RunInterval:        30 * time.Minute,
		HTTPAddr:           ":8080",
		LogFormat:          "text",
		UserAgent:          "Wera/0.1 (personal job tracker; contact: jessechumo@gmail.com)",
		MaxCostPerRunUSD:   0.50,
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
	str("HTTP_ADDR", &e.HTTPAddr)
	str("LOG_FORMAT", &e.LogFormat)
	str("USER_AGENT", &e.UserAgent)
	floatVal("MAX_COST_PER_RUN_USD", &e.MaxCostPerRunUSD)

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
	if e.LogFormat != "text" && e.LogFormat != "json" {
		errs = append(errs, fmt.Sprintf("LOG_FORMAT must be text or json, got %q", e.LogFormat))
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

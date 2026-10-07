// Package metrics defines Wera's Prometheus collectors (PLAN.md section
// 10) on a private registry, so `wera serve` and the worker expose only
// our own metrics.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry bundles every Wera collector.
type Registry struct {
	Reg *prometheus.Registry

	FetchTotal     *prometheus.CounterVec // {company, result}
	FetchDuration  prometheus.Histogram   // seconds
	JobsNewTotal   prometheus.Counter
	JobsExcluded   *prometheus.CounterVec // {reason}
	LLMRequests    *prometheus.CounterVec // {model, result}
	LLMTokens      *prometheus.CounterVec // {type}
	LLMCost        prometheus.Counter     // USD
	LLMLatency     prometheus.Histogram   // seconds
	RunLastSuccess prometheus.Gauge       // unix seconds
}

func newVecCounter(reg *prometheus.Registry, name, help string, labels []string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	reg.MustRegister(c)
	return c
}

// New builds the registry with all collectors registered.
func New() *Registry {
	reg := prometheus.NewRegistry()
	r := &Registry{Reg: reg}

	r.FetchTotal = newVecCounter(reg, "wera_fetch_total",
		"Job board fetches by company and result (ok|error).", []string{"company", "result"})
	r.FetchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "wera_fetch_duration_seconds", Help: "Job board fetch duration.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})
	reg.MustRegister(r.FetchDuration)

	r.JobsNewTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "wera_jobs_new_total", Help: "New jobs inserted by fetch runs."})
	reg.MustRegister(r.JobsNewTotal)

	r.JobsExcluded = newVecCounter(reg, "wera_jobs_excluded_total",
		"Jobs excluded by the rule filter or post-LLM exclusions, by reason.", []string{"reason"})

	r.LLMRequests = newVecCounter(reg, "wera_llm_requests_total",
		"LLM scoring requests by model and result (scored|excluded|score_failed|skipped).",
		[]string{"model", "result"})
	r.LLMTokens = newVecCounter(reg, "wera_llm_tokens_total",
		"LLM tokens by type (prompt|cached|completion).", []string{"type"})

	r.LLMCost = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "wera_llm_cost_usd_total", Help: "Total LLM cost in USD."})
	reg.MustRegister(r.LLMCost)

	r.LLMLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "wera_llm_latency_seconds", Help: "LLM scoring call latency.",
		Buckets: []float64{0.25, 0.5, 1, 2, 5, 10, 30, 60},
	})
	reg.MustRegister(r.LLMLatency)

	r.RunLastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "wera_run_last_success_timestamp",
		Help: "Unix timestamp of the last successful pipeline run."})
	reg.MustRegister(r.RunLastSuccess)

	return r
}

// HTTPHandler exposes the registry in Prometheus text format.
func (r *Registry) HTTPHandler() http.Handler {
	return promhttp.HandlerFor(r.Reg, promhttp.HandlerOpts{})
}

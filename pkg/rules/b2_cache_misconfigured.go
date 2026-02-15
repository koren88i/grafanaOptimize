package rules

// CacheMisconfigured detects Thanos query-frontend cache misconfigurations.
// This rule requires a live Prometheus endpoint to check cache hit rate metrics.
type CacheMisconfigured struct{}

func (r *CacheMisconfigured) ID() string            { return "B2" }
func (r *CacheMisconfigured) RuleSeverity() Severity { return High }

func (r *CacheMisconfigured) Check(ctx *AnalysisContext) []Finding {
	// This rule requires live Prometheus metrics to check cache hit rates.
	// Without a live endpoint, we cannot determine cache health.
	if ctx.PrometheusURL == "" {
		return nil
	}

	// TODO: Query thanos_query_frontend_queries_total{result="hit"} vs total
	// to compute cache hit rate. Flag if hit rate < 50% or metrics are absent.

	return nil
}

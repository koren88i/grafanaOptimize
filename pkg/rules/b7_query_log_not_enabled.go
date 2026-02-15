package rules

// QueryLogNotEnabled detects when Prometheus query logging is not configured.
// Without query logging, there's no way to identify slow or expensive queries
// from historical data.
type QueryLogNotEnabled struct{}

func (r *QueryLogNotEnabled) ID() string            { return "B7" }
func (r *QueryLogNotEnabled) RuleSeverity() Severity { return Medium }

func (r *QueryLogNotEnabled) Check(ctx *AnalysisContext) []Finding {
	// This rule requires a live endpoint to check Prometheus configuration.
	if ctx.PrometheusURL == "" {
		return nil
	}

	// TODO: Query /api/v1/status/config and check for query_log_file setting.
	// If empty or absent, query logging is not enabled.

	return nil
}

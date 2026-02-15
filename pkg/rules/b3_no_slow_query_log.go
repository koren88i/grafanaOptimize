package rules

// NoSlowQueryLog detects when Thanos query-frontend slow query logging is
// not enabled. Without slow query logging, there's no visibility into which
// queries are causing performance problems.
type NoSlowQueryLog struct{}

func (r *NoSlowQueryLog) ID() string            { return "B3" }
func (r *NoSlowQueryLog) RuleSeverity() Severity { return Medium }

func (r *NoSlowQueryLog) Check(ctx *AnalysisContext) []Finding {
	// This rule requires a live endpoint to check configuration.
	if ctx.PrometheusURL == "" {
		return nil
	}

	// TODO: Query /api/v1/status/flags and check for
	// --query-frontend.log-queries-longer-than being set to 0 (default/disabled).

	return nil
}

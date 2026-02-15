package rules

// DeduplicationOverhead detects dashboards querying Thanos where deduplication
// may add significant overhead. Thanos deduplication is necessary for HA setups
// but adds CPU cost proportional to the number of replica series.
type DeduplicationOverhead struct{}

func (r *DeduplicationOverhead) ID() string            { return "B5" }
func (r *DeduplicationOverhead) RuleSeverity() Severity { return Medium }

func (r *DeduplicationOverhead) Check(ctx *AnalysisContext) []Finding {
	if !dashboardUsesThanos(ctx) {
		return nil
	}

	// Static inference: if dashboard uses Thanos, deduplication is a concern.
	// This is a low-confidence advisory finding.
	return []Finding{
		{
			RuleID:      "B5",
			Severity:    Medium,
			Title:       "Thanos deduplication overhead",
			Why:         "Dashboard queries a Thanos datasource. Thanos deduplication processes every replica series before returning results, adding CPU overhead proportional to replica count.",
			Fix:         "Ensure deduplication is configured correctly (--query.replica-label). For dashboards that don't need dedup, consider querying Prometheus directly.",
			Impact:      "Correct deduplication config avoids processing unnecessary replica series",
			Validate:    "Check thanos_query_deduplicated_series_total vs thanos_query_series_total ratio",
			AutoFixable: false,
			Confidence:  0.4,
		},
	}
}

package rules

import "fmt"

// HighCardinality detects when the Prometheus TSDB has more than 1 million
// active head series. High cardinality increases memory usage, slows compaction,
// and makes all queries more expensive.
type HighCardinality struct{}

func (r *HighCardinality) ID() string            { return "B6" }
func (r *HighCardinality) RuleSeverity() Severity { return High }

const highCardinalityThreshold = 1_000_000

func (r *HighCardinality) Check(ctx *AnalysisContext) []Finding {
	// This rule requires live TSDB status data.
	if ctx.Cardinality == nil {
		return nil
	}
	if ctx.Cardinality.HeadSeriesCount <= highCardinalityThreshold {
		return nil
	}

	return []Finding{
		{
			RuleID:      "B6",
			Severity:    High,
			Title:       "High cardinality TSDB",
			Why:         fmt.Sprintf("Prometheus has %d active head series (threshold: %d). High cardinality increases memory usage, slows compaction, and makes queries more expensive.", ctx.Cardinality.HeadSeriesCount, highCardinalityThreshold),
			Fix:         "Identify and reduce high-cardinality metrics using TSDB status API. Common causes: unbounded label values (request IDs, user IDs), label explosion from relabeling, unused metrics.",
			Impact:      "Reducing head series below 1M significantly improves query performance and reduces Prometheus memory footprint",
			Validate:    "Check prometheus_tsdb_head_series metric after cleanup",
			AutoFixable: false,
			Confidence:  0.95,
		},
	}
}

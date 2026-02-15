package rules

import (
	"fmt"

	"github.com/prometheus/prometheus/promql/parser"
)

// LateAggregation detects aggregations that wrap unfiltered vector selectors.
// When an aggregation like sum() wraps a bare metric with no label matchers,
// Prometheus must first fetch every series for that metric and then aggregate
// them — the opposite of pushing filters down as early as possible.
type LateAggregation struct{}

func (r *LateAggregation) ID() string            { return "Q5" }
func (r *LateAggregation) RuleSeverity() Severity { return Medium }

func (r *LateAggregation) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				agg, ok := node.(*parser.AggregateExpr)
				if !ok {
					return nil
				}
				// Walk the inner expression looking for unfiltered VectorSelectors
				if hasUnfilteredSelector(agg.Expr) {
					metricName := extractMetricFromInner(agg.Expr)
					confidence := 0.75
					impact := "Pushes filtering earlier, reducing series fetched by orders of magnitude"
					why := fmt.Sprintf("An aggregation wraps the metric %q which has no label filters. Prometheus must fetch all series first, then aggregate — wasting memory and I/O.", metricName)

					if ctx.Cardinality != nil {
						if seriesCount := ctx.Cardinality.EstimatedSeries(metricName, 0); seriesCount > 0 {
							confidence = 0.9
							why = fmt.Sprintf("An aggregation wraps the metric %q (%d active series) with no label filters. Prometheus fetches all %d series first, then aggregates — wasting memory and I/O.", metricName, seriesCount, seriesCount)
							impact = fmt.Sprintf("Adding filters before aggregation could avoid scanning %d series unnecessarily", seriesCount)
						}
					}

					findings = append(findings, Finding{
						RuleID:      "Q5",
						Severity:    Medium,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "Late aggregation over unfiltered selector",
						Why:         why,
						Fix:         fmt.Sprintf("Add label matchers to %s before aggregating, e.g. %s{namespace=\"...\"}.", metricName, metricName),
						Impact:      impact,
						Validate:    "Query Inspector → Stats tab → compare 'Series fetched' before/after",
						AutoFixable: false,
						Confidence:  confidence,
					})
				}
				return nil
			})
		}
	}
	return findings
}

// hasUnfilteredSelector returns true if any VectorSelector in the expression
// tree has zero non-__name__ matchers.
func hasUnfilteredSelector(expr parser.Expr) bool {
	found := false
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		vs, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		realMatchers := 0
		for _, m := range vs.LabelMatchers {
			if m.Name != "__name__" {
				realMatchers++
			}
		}
		if realMatchers <= 0 {
			found = true
		}
		return nil
	})
	return found
}

// extractMetricFromInner tries to find a metric name from the inner expression.
func extractMetricFromInner(expr parser.Expr) string {
	name := "<unknown>"
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		vs, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		if vs.Name != "" {
			name = vs.Name
			return nil
		}
		for _, m := range vs.LabelMatchers {
			if m.Name == "__name__" {
				name = m.Value
				return nil
			}
		}
		return nil
	})
	return name
}

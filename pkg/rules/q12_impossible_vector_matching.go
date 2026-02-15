package rules

import (
	"fmt"

	"github.com/prometheus/prometheus/promql/parser"
)

// ImpossibleVectorMatching detects binary expressions between different metrics
// without explicit on()/ignoring() label lists. When two sides of a binary
// operation have different metrics, Prometheus matches on ALL labels by default,
// which often produces empty results or unexpected matches.
type ImpossibleVectorMatching struct{}

func (r *ImpossibleVectorMatching) ID() string            { return "Q12" }
func (r *ImpossibleVectorMatching) RuleSeverity() Severity { return Medium }

func (r *ImpossibleVectorMatching) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				binExpr, ok := node.(*parser.BinaryExpr)
				if !ok {
					return nil
				}
				// Skip logical/set operations (and, or, unless) — they always vector match
				if binExpr.Op == parser.LAND || binExpr.Op == parser.LOR || binExpr.Op == parser.LUNLESS {
					return nil
				}
				// Skip if explicit matching is configured
				if binExpr.VectorMatching != nil && len(binExpr.VectorMatching.MatchingLabels) > 0 {
					return nil
				}

				leftMetric := primaryMetricName(binExpr.LHS)
				rightMetric := primaryMetricName(binExpr.RHS)

				// Only flag if both sides are metric queries with different metrics
				if leftMetric == "" || rightMetric == "" || leftMetric == rightMetric {
					return nil
				}

				findings = append(findings, Finding{
					RuleID:      "Q12",
					Severity:    Medium,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "Binary operation without explicit label matching",
					Why:         fmt.Sprintf("Binary %s between %q and %q without on()/ignoring(). Prometheus matches on ALL labels, which may produce empty results if the two metrics have different label sets.", binExpr.Op, leftMetric, rightMetric),
					Fix:         fmt.Sprintf("Add explicit matching: ... %s on(common_labels) ..., or use ignoring(differing_labels).", binExpr.Op),
					Impact:      "Explicit matching prevents silent empty results and makes the query's intent clear",
					Validate:    "Run the query and verify it returns the expected number of series",
					AutoFixable: false,
					Confidence:  0.7,
				})
				return nil
			})
		}
	}
	return findings
}

// primaryMetricName extracts the metric name from the outermost vector selector
// in an expression. Returns empty string if the expression is complex
// (aggregations, functions with scalar results, etc.).
func primaryMetricName(expr parser.Expr) string {
	switch n := expr.(type) {
	case *parser.VectorSelector:
		return n.Name
	case *parser.MatrixSelector:
		return primaryMetricName(n.VectorSelector)
	case *parser.Call:
		// For functions like rate(), the metric is in the first arg
		if len(n.Args) > 0 {
			return primaryMetricName(n.Args[0])
		}
	case *parser.ParenExpr:
		return primaryMetricName(n.Expr)
	case *parser.StepInvariantExpr:
		return primaryMetricName(n.Expr)
	}
	return ""
}

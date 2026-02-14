package rules

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"
)

// highCardinalityLabels is the set of label names that typically have very
// high cardinality and should not appear in group-by clauses.
var highCardinalityLabels = map[string]bool{
	"pod":            true,
	"container":      true,
	"instance":       true,
	"pod_name":       true,
	"container_name": true,
	"id":             true,
	"uid":            true,
}

// HighCardinalityGrouping detects aggregation expressions that group by too
// many labels or by labels known to have very high cardinality. Such queries
// produce huge result sets that stress both Prometheus and the browser.
type HighCardinalityGrouping struct{}

func (r *HighCardinalityGrouping) ID() string            { return "Q4" }
func (r *HighCardinalityGrouping) RuleSeverity() Severity { return High }

func (r *HighCardinalityGrouping) Check(ctx *AnalysisContext) []Finding {
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
				// Check for too many grouping labels
				if len(agg.Grouping) > 3 {
					findings = append(findings, Finding{
						RuleID:      "Q4",
						Severity:    High,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "High-cardinality grouping",
						Why:         fmt.Sprintf("Aggregation groups by %d labels (%s). More than 3 grouping labels often produces an explosion of output series.", len(agg.Grouping), strings.Join(agg.Grouping, ", ")),
						Fix:         "Reduce the number of grouping labels to only those needed for the visualization.",
						Impact:      "Fewer output series reduces memory, network, and rendering cost",
						Validate:    "Query Inspector → Stats tab → check result series count before/after",
						AutoFixable: false,
						Confidence:  0.8,
					})
				}
				// Check for known high-cardinality labels
				for _, lbl := range agg.Grouping {
					if highCardinalityLabels[lbl] {
						findings = append(findings, Finding{
							RuleID:      "Q4",
							Severity:    High,
							PanelIDs:    []int{panel.ID},
							PanelTitles: []string{panel.Title},
							Title:       "High-cardinality grouping label",
							Why:         fmt.Sprintf("Aggregation groups by %q, which is typically a very high-cardinality label. This can produce thousands of output series.", lbl),
							Fix:         fmt.Sprintf("Remove %q from the group-by clause or replace it with a lower-cardinality label (e.g. namespace, job).", lbl),
							Impact:      "Dramatically reduces the number of output series",
							Validate:    "Query Inspector → Stats tab → check result series count before/after",
							AutoFixable: false,
							Confidence:  0.85,
						})
					}
				}
				return nil
			})
		}
	}
	return findings
}

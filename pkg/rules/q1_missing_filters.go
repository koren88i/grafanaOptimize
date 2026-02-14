package rules

import (
	"fmt"

	"github.com/prometheus/prometheus/promql/parser"
)

// MissingFilters detects PromQL queries with bare metrics or insufficient
// label matchers. Without filters, a query scans all series for a metric,
// which can be extremely expensive at scale.
type MissingFilters struct{}

func (r *MissingFilters) ID() string            { return "Q1" }
func (r *MissingFilters) RuleSeverity() Severity { return Critical }

func (r *MissingFilters) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				vs, ok := node.(*parser.VectorSelector)
				if !ok {
					return nil
				}
				// Count matchers excluding __name__ (which is implicit)
				realMatchers := 0
				for _, m := range vs.LabelMatchers {
					if m.Name != "__name__" {
						realMatchers++
					}
				}
				if realMatchers > 0 {
					return nil
				}
				metricName := vs.Name
				if metricName == "" && len(vs.LabelMatchers) > 0 {
					for _, m := range vs.LabelMatchers {
						if m.Name == "__name__" {
							metricName = m.Value
							break
						}
					}
				}
				findings = append(findings, Finding{
					RuleID:      "Q1",
					Severity:    Critical,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "Missing label filters",
					Why:         fmt.Sprintf("Query selects all series for metric %q without any label filters. This forces a full scan across all label combinations.", metricName),
					Fix:         fmt.Sprintf("Add label matchers to narrow the selection, e.g. %s{job=\"...\", namespace=\"...\"}", metricName),
					Impact:      "Reduces series scanned by ~10-100x depending on cardinality",
					Validate:    "Query Inspector → Stats tab → check 'Series fetched' before/after",
					AutoFixable: false,
					Confidence:  0.9,
				})
				return nil
			})
		}
	}
	return findings
}

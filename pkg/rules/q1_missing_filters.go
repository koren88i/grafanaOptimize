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
				confidence := 0.9
				impact := "Reduces series scanned by ~10-100x depending on cardinality"
				why := fmt.Sprintf("Query selects all series for metric %q without any label filters. This forces a full scan across all label combinations.", metricName)

				if ctx.Cardinality != nil {
					if seriesCount := ctx.Cardinality.EstimatedSeries(metricName, 0); seriesCount > 0 {
						confidence = 0.95
						why = fmt.Sprintf("Query selects all %d series for metric %q without any label filters. This forces a full scan across all label combinations.", seriesCount, metricName)
						impact = fmt.Sprintf("This metric has %d active series — adding filters could reduce scans by 10-100x", seriesCount)
					}
				}

				findings = append(findings, Finding{
					RuleID:      "Q1",
					Severity:    Critical,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "Missing label filters",
					Why:         why,
					Fix:         fmt.Sprintf("Add label matchers to narrow the selection, e.g. %s{job=\"...\", namespace=\"...\"}", metricName),
					Impact:      impact,
					Validate:    "Query Inspector → Stats tab → check 'Series fetched' before/after",
					AutoFixable: false,
					Confidence:  confidence,
				})
				return nil
			})
		}
	}
	return findings
}

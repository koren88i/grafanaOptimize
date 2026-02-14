package rules

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// SubqueryAbuse detects subquery expressions that are nested, have an
// unreasonably fine step relative to a long range, or have a range/step
// ratio that is too large. Subqueries are evaluated recursively and can
// multiply the work Prometheus must do exponentially.
type SubqueryAbuse struct{}

func (r *SubqueryAbuse) ID() string            { return "Q8" }
func (r *SubqueryAbuse) RuleSeverity() Severity { return High }

func (r *SubqueryAbuse) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				sq, ok := node.(*parser.SubqueryExpr)
				if !ok {
					return nil
				}

				// (a) Nested subquery — inner expression is also a SubqueryExpr
				if isNestedSubquery(sq.Expr) {
					findings = append(findings, Finding{
						RuleID:      "Q8",
						Severity:    High,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "Nested subquery",
						Why:         "A subquery is nested inside another subquery. Nested subqueries cause exponential evaluation cost and can overwhelm Prometheus.",
						Fix:         "Flatten the subquery or use recording rules to pre-compute intermediate results.",
						Impact:      "Avoids exponential evaluation cost",
						Validate:    "Query Inspector → Stats tab → compare query time before/after",
						AutoFixable: false,
						Confidence:  0.95,
					})
				}

				// (b) Fine step with long range: step < 1m AND range > 1h
				if sq.Step > 0 && sq.Step < time.Minute && sq.Range > time.Hour {
					findings = append(findings, Finding{
						RuleID:      "Q8",
						Severity:    High,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "Subquery with fine step over long range",
						Why:         fmt.Sprintf("Subquery has a %s step over a %s range. This produces %d evaluation points, creating excessive load.", sq.Step, sq.Range, int(sq.Range/sq.Step)),
						Fix:         "Increase the step or reduce the range. Consider using a recording rule for long-range aggregations.",
						Impact:      "Dramatically reduces the number of inner evaluations",
						Validate:    "Query Inspector → Stats tab → compare query time and samples before/after",
						AutoFixable: false,
						Confidence:  0.9,
					})
				}

				// (c) Range/step ratio > 360
				if sq.Step > 0 {
					ratio := int(sq.Range / sq.Step)
					if ratio > 360 {
						findings = append(findings, Finding{
							RuleID:      "Q8",
							Severity:    High,
							PanelIDs:    []int{panel.ID},
							PanelTitles: []string{panel.Title},
							Title:       "Subquery with excessive range/step ratio",
							Why:         fmt.Sprintf("Subquery range/step ratio is %d (range=%s, step=%s). Ratios above 360 cause excessive evaluation points.", ratio, sq.Range, sq.Step),
							Fix:         "Increase the step or reduce the range to bring the ratio under 360.",
							Impact:      "Reduces the number of evaluation points to a manageable level",
							Validate:    "Query Inspector → Stats tab → compare query time before/after",
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

// isNestedSubquery checks if expr directly or indirectly contains another SubqueryExpr.
func isNestedSubquery(expr parser.Expr) bool {
	found := false
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		if _, ok := node.(*parser.SubqueryExpr); ok {
			found = true
		}
		return nil
	})
	return found
}

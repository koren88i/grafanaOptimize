package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"
)

// incorrectAggOrderRe matches patterns like rate(sum(...)), irate(sum(...)),
// increase(sum(...)) in raw expression strings. This is the most common form
// of incorrect aggregation ordering.
var incorrectAggOrderRe = regexp.MustCompile(`(?:rate|irate|increase)\s*\(\s*(?:sum|avg|min|max|count)\s*\(`)

// IncorrectAggregation detects cases where rate/irate/increase is applied
// after an aggregation (e.g. rate(sum(x)[5m])). This is mathematically wrong
// because rate() expects monotonically increasing counter values, but
// aggregation output does not preserve that invariant. The correct order is
// sum(rate(x[5m])).
type IncorrectAggregation struct{}

func (r *IncorrectAggregation) ID() string            { return "Q10" }
func (r *IncorrectAggregation) RuleSeverity() Severity { return Medium }

func (r *IncorrectAggregation) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			rawExpr := target.Expr

			// Strategy 1: String-level detection for patterns like rate(sum(
			if incorrectAggOrderRe.MatchString(rawExpr) {
				match := incorrectAggOrderRe.FindString(rawExpr)
				outerFunc := extractFuncName(match)
				findings = append(findings, Finding{
					RuleID:      "Q10",
					Severity:    Medium,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "Incorrect aggregation order",
					Why:         fmt.Sprintf("Expression applies %s() over an aggregation. Rate-like functions expect raw counter values, but aggregation output is not a monotonic counter — results will be mathematically incorrect.", outerFunc),
					Fix:         fmt.Sprintf("Reverse the order: apply %s() first on the raw metric, then aggregate. E.g. sum(rate(metric[5m])) instead of rate(sum(metric)[5m]).", outerFunc),
					Impact:      "Produces mathematically correct results and often reduces series scanned",
					Validate:    "Compare the output values — after fixing, the graph shape should be similar but values will be accurate",
					AutoFixable: false,
					Confidence:  0.85,
				})
				continue
			}

			// Strategy 2: AST-level detection for rate/irate/increase wrapping
			// a SubqueryExpr whose inner expression is an AggregateExpr.
			expr, ok := ctx.ParsedExprs[rawExpr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				call, ok := node.(*parser.Call)
				if !ok {
					return nil
				}
				if !isRateLikeFunc(call.Func.Name) {
					return nil
				}
				for _, arg := range call.Args {
					sq, ok := arg.(*parser.SubqueryExpr)
					if !ok {
						continue
					}
					if containsAggregateExpr(sq.Expr) {
						findings = append(findings, Finding{
							RuleID:      "Q10",
							Severity:    Medium,
							PanelIDs:    []int{panel.ID},
							PanelTitles: []string{panel.Title},
							Title:       "Incorrect aggregation order",
							Why:         fmt.Sprintf("Expression applies %s() over a subquery containing an aggregation. Rate-like functions expect raw counter values, but aggregation output is not a monotonic counter.", call.Func.Name),
							Fix:         fmt.Sprintf("Reverse the order: apply %s() first on the raw metric, then aggregate.", call.Func.Name),
							Impact:      "Produces mathematically correct results and often reduces series scanned",
							Validate:    "Compare the output values — after fixing, the graph shape should be similar but values will be accurate",
							AutoFixable: false,
							Confidence:  0.8,
						})
					}
				}
				return nil
			})
		}
	}
	return findings
}

// extractFuncName extracts the outer function name from a regex match.
func extractFuncName(match string) string {
	for _, fn := range []string{"rate", "irate", "increase"} {
		if strings.HasPrefix(match, fn) {
			return fn
		}
	}
	return "rate"
}

// isRateLikeFunc returns true if the function name is rate, irate, or increase.
func isRateLikeFunc(name string) bool {
	switch name {
	case "rate", "irate", "increase":
		return true
	}
	return false
}

// containsAggregateExpr returns true if the expression tree contains an AggregateExpr.
func containsAggregateExpr(expr parser.Expr) bool {
	found := false
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		if _, ok := node.(*parser.AggregateExpr); ok {
			found = true
		}
		return nil
	})
	return found
}

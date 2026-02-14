package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"
)

// hardcodedRangeRe matches rate/irate/increase calls with hardcoded time
// durations like [5m], [1h], [30s] instead of [$__rate_interval] or [$__interval].
var hardcodedRangeRe = regexp.MustCompile(`(?:rate|irate|increase)\s*\([^)]*\[\d+[smh]\]`)

// rateFuncsForInterval is the set of functions that should use $__rate_interval
// or $__interval instead of hardcoded durations.
var rateFuncsForInterval = map[string]bool{
	"rate":     true,
	"irate":    true,
	"increase": true,
}

// HardcodedInterval detects rate/irate/increase calls that use hardcoded
// time durations instead of Grafana's $__rate_interval or $__interval
// template variables. Hardcoded intervals break when the dashboard time
// range or scrape interval changes, often producing wrong or missing data.
type HardcodedInterval struct{}

func (r *HardcodedInterval) ID() string            { return "Q7" }
func (r *HardcodedInterval) RuleSeverity() Severity { return Medium }

func (r *HardcodedInterval) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			rawExpr := target.Expr
			// Check if this expression contains a rate/irate/increase call
			expr, ok := ctx.ParsedExprs[rawExpr]
			if !ok {
				continue
			}
			hasRateFunc := false
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				call, ok := node.(*parser.Call)
				if !ok {
					return nil
				}
				if rateFuncsForInterval[call.Func.Name] {
					hasRateFunc = true
				}
				return nil
			})
			if !hasRateFunc {
				continue
			}
			// If the raw expression already uses the template variables, skip it
			if strings.Contains(rawExpr, "$__rate_interval") || strings.Contains(rawExpr, "$__interval") {
				continue
			}
			// Check for hardcoded intervals in the raw expression
			if hardcodedRangeRe.MatchString(rawExpr) {
				matches := hardcodedRangeRe.FindString(rawExpr)
				funcName := "rate"
				for _, fn := range []string{"rate", "irate", "increase"} {
					if strings.HasPrefix(matches, fn) {
						funcName = fn
						break
					}
				}
				findings = append(findings, Finding{
					RuleID:      "Q7",
					Severity:    Medium,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "Hardcoded interval in rate function",
					Why:         fmt.Sprintf("%s() uses a hardcoded duration instead of $__rate_interval or $__interval. This breaks when the dashboard time range or scrape interval changes.", funcName),
					Fix:         fmt.Sprintf("Replace the hardcoded duration with $__rate_interval, e.g. %s(metric[$__rate_interval]).", funcName),
					Impact:      "Ensures correct per-point calculations regardless of time range or scrape config",
					Validate:    "Change the dashboard time range and verify the panel still renders correctly",
					AutoFixable: true,
					Confidence:  0.9,
				})
			}
		}
	}
	return findings
}

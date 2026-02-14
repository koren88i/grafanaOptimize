package rules

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// rateFuncNames is the set of PromQL functions that take a range vector
// as their first argument. Excessively long ranges cause these functions
// to process huge amounts of data.
var rateFuncNames = map[string]bool{
	"rate":     true,
	"irate":    true,
	"increase": true,
	"delta":    true,
	"idelta":   true,
}

// LongRateRange detects rate/irate/increase/delta/idelta calls where the
// range vector window exceeds 10 minutes. Long ranges force Prometheus to
// read and iterate over many more samples per series, increasing CPU and
// memory usage.
type LongRateRange struct{}

func (r *LongRateRange) ID() string            { return "Q6" }
func (r *LongRateRange) RuleSeverity() Severity { return Medium }

func (r *LongRateRange) Check(ctx *AnalysisContext) []Finding {
	const threshold = 10 * time.Minute
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				call, ok := node.(*parser.Call)
				if !ok {
					return nil
				}
				if !rateFuncNames[call.Func.Name] {
					return nil
				}
				if len(call.Args) == 0 {
					return nil
				}
				ms, ok := call.Args[0].(*parser.MatrixSelector)
				if !ok {
					return nil
				}
				if ms.Range > threshold {
					findings = append(findings, Finding{
						RuleID:      "Q6",
						Severity:    Medium,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "Long rate range",
						Why:         fmt.Sprintf("%s() uses a %s range window. Windows longer than 10m force Prometheus to scan many more samples per series.", call.Func.Name, ms.Range),
						Fix:         fmt.Sprintf("Reduce the range to match the scrape interval or use $__rate_interval. E.g. %s(metric[5m]).", call.Func.Name),
						Impact:      "Reduces the number of samples processed per evaluation, lowering CPU and memory",
						Validate:    "Query Inspector → Stats tab → compare query time before/after",
						AutoFixable: false,
						Confidence:  0.8,
					})
				}
				return nil
			})
		}
	}
	return findings
}

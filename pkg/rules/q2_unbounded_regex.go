package rules

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// UnboundedRegex detects label matchers using regex patterns that match too
// broadly. Patterns like `.*foo`, `foo.*bar`, or `.+` force the regex engine
// to scan enormous numbers of label values, causing high memory and CPU usage.
type UnboundedRegex struct{}

func (r *UnboundedRegex) ID() string            { return "Q2" }
func (r *UnboundedRegex) RuleSeverity() Severity { return High }

func (r *UnboundedRegex) Check(ctx *AnalysisContext) []Finding {
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
				for _, m := range vs.LabelMatchers {
					if m.Name == "__name__" {
						continue
					}
					if m.Type != labels.MatchRegexp {
						continue
					}
					reason := unboundedRegexReason(m.Value)
					if reason == "" {
						continue
					}
					findings = append(findings, Finding{
						RuleID:      "Q2",
						Severity:    High,
						PanelIDs:    []int{panel.ID},
						PanelTitles: []string{panel.Title},
						Title:       "Unbounded regex matcher",
						Why:         fmt.Sprintf("Label %q uses regex =~%q — %s. This can force a full scan of all label values.", m.Name, m.Value, reason),
						Fix:         fmt.Sprintf("Rewrite the regex for %s to be more specific, e.g. use a prefix match or equality.", m.Name),
						Impact:      "Reduces label value scanning and regex evaluation overhead significantly",
						Validate:    "Query Inspector → Stats tab → compare 'Series fetched' before/after",
						AutoFixable: false,
						Confidence:  0.85,
					})
				}
				return nil
			})
		}
	}
	return findings
}

// unboundedRegexReason returns a human-readable reason if the regex value
// is unbounded, or "" if it looks fine.
func unboundedRegexReason(value string) string {
	if value == ".+" {
		return "pattern .+ matches every non-empty label value"
	}
	if strings.HasPrefix(value, ".*") {
		return "leading .* causes a full scan of all label values"
	}
	// Check for .* in the middle (not at start or end)
	// Trim any trailing .* since that's anchored anyway
	trimmed := strings.TrimSuffix(value, ".*")
	if idx := strings.Index(trimmed, ".*"); idx > 0 {
		return "mid-pattern .* causes expensive backtracking"
	}
	return ""
}

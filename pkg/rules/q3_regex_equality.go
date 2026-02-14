package rules

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// RegexEquality detects label matchers using =~ with values that contain no
// regex metacharacters. These should use = instead, avoiding regex engine
// overhead on every label lookup.
type RegexEquality struct{}

func (r *RegexEquality) ID() string            { return "Q3" }
func (r *RegexEquality) RuleSeverity() Severity { return Medium }

func (r *RegexEquality) Check(ctx *AnalysisContext) []Finding {
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
					if m.Type == labels.MatchRegexp && !containsRegexMeta(m.Value) {
						findings = append(findings, Finding{
							RuleID:      "Q3",
							Severity:    Medium,
							PanelIDs:    []int{panel.ID},
							PanelTitles: []string{panel.Title},
							Title:       "Regex matcher where equality suffices",
							Why:         fmt.Sprintf("Label %q uses regex match =~%q but the value contains no regex metacharacters. Regex matching is slower than equality.", m.Name, m.Value),
							Fix:         fmt.Sprintf("Change %s=~\"%s\" to %s=\"%s\"", m.Name, m.Value, m.Name, m.Value),
							Impact:      "Avoids regex engine overhead on every label lookup",
							Validate:    "Query Inspector → Stats tab → compare query time before/after",
							AutoFixable: true,
							Confidence:  1.0,
						})
					}
				}
				return nil
			})
		}
	}
	return findings
}

// containsRegexMeta returns true if s contains regex metacharacters.
func containsRegexMeta(s string) bool {
	for _, c := range s {
		switch c {
		case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '\\':
			return true
		}
	}
	return false
}

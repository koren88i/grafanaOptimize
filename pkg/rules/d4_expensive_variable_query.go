package rules

import (
	"fmt"
	"strings"
)

// ExpensiveVariableQuery detects template variables of type "query" that use
// a full PromQL expression instead of the lightweight label_values() function.
// Full PromQL variable queries execute a real query against Prometheus on every
// dashboard load or variable refresh, which is much more expensive than
// label_values() which reads only label metadata.
type ExpensiveVariableQuery struct{}

func (r *ExpensiveVariableQuery) ID() string            { return "D4" }
func (r *ExpensiveVariableQuery) RuleSeverity() Severity { return High }

func (r *ExpensiveVariableQuery) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, v := range ctx.Variables {
		if v.Type != "query" {
			continue
		}
		qs := strings.TrimSpace(v.QueryString())
		if qs == "" {
			continue
		}
		// label_values(...) is a lightweight metadata call.
		if strings.HasPrefix(qs, "label_values(") {
			continue
		}
		findings = append(findings, Finding{
			RuleID:   "D4",
			Severity: High,
			Title:    "Variable uses full PromQL query",
			Why: fmt.Sprintf(
				"Variable $%s uses query %q instead of label_values(). "+
					"Full PromQL queries run against Prometheus on every variable refresh, "+
					"causing unnecessary load.",
				v.Name, truncateQuery(qs, 80),
			),
			Fix:         fmt.Sprintf("Rewrite variable $%s to use label_values(<metric>, <label>) if possible.", v.Name),
			Impact:      "Replaces a full query execution with a lightweight metadata lookup on each dashboard load",
			Validate:    "Open dashboard → check Network tab for variable query timing",
			AutoFixable: false,
			Confidence:  0.9,
		})
	}
	return findings
}

// truncateQuery shortens a query string for display purposes.
func truncateQuery(q string, maxLen int) string {
	if len(q) <= maxLen {
		return q
	}
	return q[:maxLen-3] + "..."
}

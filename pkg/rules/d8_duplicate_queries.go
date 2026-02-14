package rules

import (
	"fmt"
	"strings"

	"github.com/dashboard-advisor/pkg/extractor"
)

// DuplicateQueries detects identical query expressions used across multiple
// panels. When the same query appears in several panels, each panel fires its
// own request to the datasource. Using the "Dashboard" datasource to share
// a single query result across panels eliminates redundant requests.
type DuplicateQueries struct{}

func (r *DuplicateQueries) ID() string            { return "D8" }
func (r *DuplicateQueries) RuleSeverity() Severity { return Medium }

func (r *DuplicateQueries) Check(ctx *AnalysisContext) []Finding {
	// Map each expression to the panels that use it.
	type panelRef struct {
		id    int
		title string
	}
	exprPanels := make(map[string][]panelRef)

	allPanels := extractor.AllPanels(ctx.Dashboard)
	for _, p := range allPanels {
		if p.Type == "row" {
			continue
		}
		for _, t := range p.Targets {
			expr := strings.TrimSpace(t.Expr)
			if expr == "" {
				continue
			}
			exprPanels[expr] = append(exprPanels[expr], panelRef{id: p.ID, title: p.Title})
		}
	}

	var findings []Finding
	for expr, panels := range exprPanels {
		if len(panels) <= 2 {
			continue
		}
		ids := make([]int, len(panels))
		titles := make([]string, len(panels))
		for i, p := range panels {
			ids[i] = p.id
			titles[i] = p.title
		}

		findings = append(findings, Finding{
			RuleID:      "D8",
			Severity:    Medium,
			PanelIDs:    ids,
			PanelTitles: titles,
			Title:       "Duplicate query across panels",
			Why: fmt.Sprintf(
				"Query %q is used in %d panels [%s]. Each panel fires its own request, causing redundant datasource load.",
				truncateQuery(expr, 80), len(panels), strings.Join(titles, ", "),
			),
			Fix:         "Use the Dashboard datasource to share the query result from one panel to the others, eliminating duplicate requests.",
			Impact:      fmt.Sprintf("Eliminates %d redundant query executions per refresh cycle", len(panels)-1),
			Validate:    "Check Network tab to confirm only one request is made for the shared query",
			AutoFixable: false,
			Confidence:  0.9,
		})
	}
	return findings
}

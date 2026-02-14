package rules

import (
	"fmt"

	"github.com/dashboard-advisor/pkg/extractor"
)

// NoCollapsedRows detects dashboards that have no collapsed row panels.
// Collapsed rows defer query execution for their nested panels until the
// row is expanded, reducing the initial query load. Dashboards without any
// collapsed rows fire all panel queries on load.
type NoCollapsedRows struct{}

func (r *NoCollapsedRows) ID() string            { return "D10" }
func (r *NoCollapsedRows) RuleSeverity() Severity { return Medium }

func (r *NoCollapsedRows) Check(ctx *AnalysisContext) []Finding {
	allPanels := extractor.AllPanels(ctx.Dashboard)

	totalPanels := 0
	for _, p := range allPanels {
		if p.Type != "row" {
			totalPanels++
		}
	}

	// Only flag dashboards that have enough panels to benefit from rows.
	// A dashboard with very few panels doesn't need collapsed rows.
	if totalPanels < 5 {
		return nil
	}

	hasRows := false
	hasCollapsedRow := false
	for _, p := range ctx.Dashboard.Panels {
		if p.Type == "row" {
			hasRows = true
			if p.Collapsed {
				hasCollapsedRow = true
				break
			}
		}
	}

	if hasCollapsedRow {
		return nil
	}

	why := ""
	if !hasRows {
		why = fmt.Sprintf(
			"Dashboard has %d panels but no row panels. "+
				"Without rows, all panels fire queries on load.",
			totalPanels,
		)
	} else {
		why = fmt.Sprintf(
			"Dashboard has %d panels with row panels, but none are collapsed. "+
				"All panels still fire queries on load because no rows defer execution.",
			totalPanels,
		)
	}

	return []Finding{
		{
			RuleID:      "D10",
			Severity:    Medium,
			Title:       "No collapsed rows to defer query execution",
			Why:         why,
			Fix:         "Organize panels into rows and collapse less-frequently viewed sections. Collapsed rows defer query execution until expanded.",
			Impact:      "Reduces initial query count by the number of panels moved into collapsed rows",
			Validate:    "Reload dashboard → verify collapsed rows show an expand arrow and don't fire queries until clicked",
			AutoFixable: false,
			Confidence:  0.8,
		},
	}
}

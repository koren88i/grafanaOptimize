package rules

import (
	"fmt"

	"github.com/dashboard-advisor/pkg/extractor"
)

// TooManyPanels detects dashboards with more than a threshold number of
// visible panels. Each visible panel fires queries on load, so too many
// panels cause slow initial render and excessive backend load.
type TooManyPanels struct {
	// Threshold is the max number of visible panels before flagging.
	// Defaults to 25 if zero.
	Threshold int
}

func (r *TooManyPanels) ID() string            { return "D1" }
func (r *TooManyPanels) RuleSeverity() Severity { return High }

func (r *TooManyPanels) threshold() int {
	if r.Threshold > 0 {
		return r.Threshold
	}
	return 25
}

func (r *TooManyPanels) Check(ctx *AnalysisContext) []Finding {
	visible := extractor.VisiblePanels(ctx.Dashboard)
	count := len(visible)
	thresh := r.threshold()

	if count <= thresh {
		return nil
	}

	return []Finding{
		{
			RuleID:      "D1",
			Severity:    High,
			Title:       "Too many visible panels",
			Why:         fmt.Sprintf("Dashboard has %d visible panels (threshold: %d). Each panel fires queries on load, causing slow initial render and high backend load.", count, thresh),
			Fix:         "Group related panels into collapsed rows, or split the dashboard into multiple focused dashboards.",
			Impact:      fmt.Sprintf("Reducing from %d to ≤%d panels cuts initial query load proportionally", count, thresh),
			Validate:    "Reload dashboard → check browser DevTools Network tab for query count",
			AutoFixable: false,
			Confidence:  1.0,
		},
	}
}

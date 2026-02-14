package rules

import (
	"fmt"

	"github.com/dashboard-advisor/pkg/extractor"
)

// panelTypesNeedingMaxDataPoints lists panel types that benefit from having
// maxDataPoints set to limit the number of data points returned by the
// datasource.
var panelTypesNeedingMaxDataPoints = map[string]bool{
	"timeseries": true,
	"graph":      true,
	"barchart":   true,
	"heatmap":    true,
}

// MissingMaxDataPoints detects time-series-style panels that do not have
// maxDataPoints configured. Without this setting, the datasource may return
// an unbounded number of data points for wide time ranges, causing slow
// rendering and excessive memory usage in the browser.
type MissingMaxDataPoints struct{}

func (r *MissingMaxDataPoints) ID() string            { return "D7" }
func (r *MissingMaxDataPoints) RuleSeverity() Severity { return Medium }

func (r *MissingMaxDataPoints) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding

	allPanels := extractor.AllPanels(ctx.Dashboard)
	for _, p := range allPanels {
		if !panelTypesNeedingMaxDataPoints[p.Type] {
			continue
		}
		if p.MaxDataPoints != nil && *p.MaxDataPoints > 0 {
			continue
		}
		findings = append(findings, Finding{
			RuleID:      "D7",
			Severity:    Medium,
			PanelIDs:    []int{p.ID},
			PanelTitles: []string{p.Title},
			Title:       "Missing maxDataPoints",
			Why:         fmt.Sprintf("Panel %q (type: %s) does not set maxDataPoints. Without this limit, the datasource may return unbounded data points for wide time ranges, causing slow rendering.", p.Title, p.Type),
			Fix:         "Set maxDataPoints in the panel's query options (e.g., 1000 for timeseries panels).",
			Impact:      "Bounds the data returned per query, reducing browser memory and render time",
			Validate:    "Open panel edit → Query Options → verify maxDataPoints is set",
			AutoFixable: true,
			Confidence:  0.9,
		})
	}
	return findings
}

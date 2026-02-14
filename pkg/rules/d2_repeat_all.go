package rules

import (
	"fmt"

	"github.com/dashboard-advisor/pkg/extractor"
)

// RepeatWithAll detects panels using the repeat feature where the
// referenced template variable has "Include All" enabled. When a panel
// repeats over a variable that includes "All", Grafana may instantiate
// hundreds of panel copies, causing massive query fan-out and slow loads.
type RepeatWithAll struct{}

func (r *RepeatWithAll) ID() string            { return "D2" }
func (r *RepeatWithAll) RuleSeverity() Severity { return Critical }

func (r *RepeatWithAll) Check(ctx *AnalysisContext) []Finding {
	// Build a lookup of variables by name.
	varByName := make(map[string]*extractor.VariableModel, len(ctx.Variables))
	for i := range ctx.Variables {
		varByName[ctx.Variables[i].Name] = &ctx.Variables[i]
	}

	var findings []Finding
	allPanels := extractor.AllPanels(ctx.Dashboard)
	for _, p := range allPanels {
		if p.Repeat == "" {
			continue
		}
		v, ok := varByName[p.Repeat]
		if !ok {
			continue
		}
		if !v.IncludeAll {
			continue
		}
		findings = append(findings, Finding{
			RuleID:      "D2",
			Severity:    Critical,
			PanelIDs:    []int{p.ID},
			PanelTitles: []string{p.Title},
			Title:       "Repeat panel uses variable with Include All",
			Why:         fmt.Sprintf("Panel %q repeats over variable $%s which has Include All enabled. Selecting All can instantiate hundreds of panel copies, each firing its own queries.", p.Title, v.Name),
			Fix:         fmt.Sprintf("Disable Include All on variable %q, or remove the repeat from this panel and use a multi-value variable filter instead.", v.Name),
			Impact:      "Prevents unbounded panel multiplication that causes query fan-out proportional to variable cardinality",
			Validate:    "Select All on the variable and check that the panel count stays reasonable",
			AutoFixable: false,
			Confidence:  1.0,
		})
	}
	return findings
}

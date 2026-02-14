package analyzer

import (
	"fmt"

	"github.com/dashboard-advisor/pkg/extractor"
	"github.com/dashboard-advisor/pkg/rules"
)

// Engine orchestrates the full analysis pipeline:
// load dashboard → extract → parse → run rules → score → report.
type Engine struct {
	rules []rules.Rule
}

// NewEngine creates an Engine with no rules registered.
func NewEngine() *Engine {
	return &Engine{}
}

// RegisterRule adds a detection rule to the engine.
func (e *Engine) RegisterRule(r rules.Rule) {
	e.rules = append(e.rules, r)
}

// DefaultEngine returns an Engine with all built-in Phase 1 rules registered.
func DefaultEngine() *Engine {
	e := NewEngine()
	// Q-series: PromQL rules
	e.RegisterRule(&rules.MissingFilters{})       // Q1
	e.RegisterRule(&rules.UnboundedRegex{})        // Q2
	e.RegisterRule(&rules.RegexEquality{})         // Q3
	e.RegisterRule(&rules.HighCardinalityGrouping{}) // Q4
	e.RegisterRule(&rules.LateAggregation{})       // Q5
	e.RegisterRule(&rules.LongRateRange{})         // Q6
	e.RegisterRule(&rules.HardcodedInterval{})     // Q7
	e.RegisterRule(&rules.SubqueryAbuse{})         // Q8
	e.RegisterRule(&rules.DuplicateExpressions{})  // Q9
	e.RegisterRule(&rules.IncorrectAggregation{})  // Q10
	// D-series: Dashboard design rules
	e.RegisterRule(&rules.TooManyPanels{})         // D1
	e.RegisterRule(&rules.RepeatWithAll{})         // D2
	e.RegisterRule(&rules.VariableExplosion{})     // D3
	e.RegisterRule(&rules.ExpensiveVariableQuery{}) // D4
	e.RegisterRule(&rules.RefreshTooFrequent{})    // D5
	e.RegisterRule(&rules.RangeTooWide{})          // D6
	e.RegisterRule(&rules.MissingMaxDataPoints{})  // D7
	e.RegisterRule(&rules.DuplicateQueries{})      // D8
	e.RegisterRule(&rules.DatasourceMixing{})      // D9
	e.RegisterRule(&rules.NoCollapsedRows{})       // D10
	return e
}

// AnalyzeFile loads a dashboard JSON file and runs the full analysis pipeline.
func (e *Engine) AnalyzeFile(path string) (*rules.Report, error) {
	dash, err := extractor.LoadDashboard(path)
	if err != nil {
		return nil, fmt.Errorf("loading dashboard: %w", err)
	}
	return e.AnalyzeDashboard(dash), nil
}

// AnalyzeDashboard runs all registered rules against a parsed dashboard.
func (e *Engine) AnalyzeDashboard(dash *extractor.DashboardModel) *rules.Report {
	allPanels := extractor.PanelsWithTargets(dash)
	allExprs := extractor.AllTargetExprs(dash)
	parsed, parseErrors := ParseAllExprs(allExprs)

	ctx := &rules.AnalysisContext{
		Dashboard:   dash,
		Panels:      allPanels,
		Variables:   dash.Templating.List,
		ParsedExprs: parsed,
	}

	var findings []rules.Finding
	for _, r := range e.rules {
		findings = append(findings, r.Check(ctx)...)
	}

	score := rules.ComputeScore(findings)
	panelScores := computePanelScores(findings)

	// Count total targets
	totalTargets := 0
	for _, p := range extractor.AllPanels(dash) {
		totalTargets += len(p.Targets)
	}

	return &rules.Report{
		DashboardUID:   dash.UID,
		DashboardTitle: dash.Title,
		Score:          score,
		Findings:       findings,
		PanelScores:    panelScores,
		Metadata: rules.ReportMetadata{
			TotalPanels:     len(extractor.AllPanels(dash)),
			TotalTargets:    totalTargets,
			ParseErrors:     len(parseErrors),
			AnalyzerVersion: "0.1.0",
		},
	}
}

// computePanelScores calculates a score for each panel that has findings.
func computePanelScores(findings []rules.Finding) map[int]int {
	// Group findings by panel ID
	panelFindings := make(map[int][]rules.Finding)
	for _, f := range findings {
		for _, pid := range f.PanelIDs {
			panelFindings[pid] = append(panelFindings[pid], f)
		}
	}

	scores := make(map[int]int, len(panelFindings))
	for pid, pf := range panelFindings {
		scores[pid] = rules.ComputeScore(pf)
	}
	return scores
}

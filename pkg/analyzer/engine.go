package analyzer

import (
	"fmt"
	"log"

	"github.com/dashboard-advisor/pkg/cardinality"
	"github.com/dashboard-advisor/pkg/extractor"
	"github.com/dashboard-advisor/pkg/rules"
)

// Engine orchestrates the full analysis pipeline:
// load dashboard → extract → parse → run rules → score → report.
type Engine struct {
	rules             []rules.Rule
	cardinalityClient *cardinality.Client // nil when --prometheus-url not provided
	prometheusURL     string              // passed through to AnalysisContext for B-rules
}

// NewEngine creates an Engine with no rules registered.
func NewEngine() *Engine {
	return &Engine{}
}

// RegisterRule adds a detection rule to the engine.
func (e *Engine) RegisterRule(r rules.Rule) {
	e.rules = append(e.rules, r)
}

// WithCardinality configures live cardinality enrichment via a Prometheus TSDB
// status API client. When set, the engine fetches cardinality data and passes
// it to rules through AnalysisContext.Cardinality.
func (e *Engine) WithCardinality(c *cardinality.Client, prometheusURL string) {
	e.cardinalityClient = c
	e.prometheusURL = prometheusURL
}

// DefaultEngine returns an Engine with all built-in rules registered.
func DefaultEngine() *Engine {
	e := NewEngine()
	// Q-series: PromQL rules
	e.RegisterRule(&rules.MissingFilters{})            // Q1
	e.RegisterRule(&rules.UnboundedRegex{})             // Q2
	e.RegisterRule(&rules.RegexEquality{})              // Q3
	e.RegisterRule(&rules.HighCardinalityGrouping{})    // Q4
	e.RegisterRule(&rules.LateAggregation{})            // Q5
	e.RegisterRule(&rules.LongRateRange{})              // Q6
	e.RegisterRule(&rules.HardcodedInterval{})          // Q7
	e.RegisterRule(&rules.SubqueryAbuse{})              // Q8
	e.RegisterRule(&rules.DuplicateExpressions{})       // Q9
	e.RegisterRule(&rules.IncorrectAggregation{})       // Q10
	e.RegisterRule(&rules.RateOnGauge{})                // Q11
	e.RegisterRule(&rules.ImpossibleVectorMatching{})   // Q12
	// D-series: Dashboard design rules
	e.RegisterRule(&rules.TooManyPanels{})              // D1
	e.RegisterRule(&rules.RepeatWithAll{})              // D2
	e.RegisterRule(&rules.VariableExplosion{})          // D3
	e.RegisterRule(&rules.ExpensiveVariableQuery{})     // D4
	e.RegisterRule(&rules.RefreshTooFrequent{})         // D5
	e.RegisterRule(&rules.RangeTooWide{})               // D6
	e.RegisterRule(&rules.MissingMaxDataPoints{})       // D7
	e.RegisterRule(&rules.DuplicateQueries{})           // D8
	e.RegisterRule(&rules.DatasourceMixing{})           // D9
	e.RegisterRule(&rules.NoCollapsedRows{})            // D10
	// B-series: Backend/infrastructure rules
	e.RegisterRule(&rules.NoQueryFrontend{})            // B1
	e.RegisterRule(&rules.CacheMisconfigured{})         // B2
	e.RegisterRule(&rules.NoSlowQueryLog{})             // B3
	e.RegisterRule(&rules.StoreGatewayNoCache{})        // B4
	e.RegisterRule(&rules.DeduplicationOverhead{})      // B5
	e.RegisterRule(&rules.HighCardinality{})            // B6
	e.RegisterRule(&rules.QueryLogNotEnabled{})         // B7
	return e
}

// AnalyzeBytes parses raw dashboard JSON bytes and runs the full analysis pipeline.
func (e *Engine) AnalyzeBytes(data []byte) (*rules.Report, error) {
	dash, err := extractor.ParseDashboard(data)
	if err != nil {
		return nil, fmt.Errorf("parsing dashboard: %w", err)
	}
	return e.AnalyzeDashboard(dash), nil
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

	// Optionally fetch cardinality data from Prometheus TSDB status API
	var cardData *cardinality.CardinalityData
	if e.cardinalityClient != nil {
		var err error
		cardData, err = e.cardinalityClient.Fetch()
		if err != nil {
			log.Printf("WARN: cardinality enrichment unavailable: %v", err)
		}
	}

	ctx := &rules.AnalysisContext{
		Dashboard:     dash,
		Panels:        allPanels,
		Variables:     dash.Templating.List,
		ParsedExprs:   parsed,
		Cardinality:   cardData,
		PrometheusURL: e.prometheusURL,
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

	// Compute query costs for ranking panels by expense
	queryCosts := make(map[string]float64, len(parsed))
	for rawExpr, expr := range parsed {
		queryCosts[rawExpr] = EstimateQueryCost(expr, cardData, 15.0)
	}

	return &rules.Report{
		DashboardUID:   dash.UID,
		DashboardTitle: dash.Title,
		Score:          score,
		Findings:       findings,
		PanelScores:    panelScores,
		Metadata: rules.ReportMetadata{
			TotalPanels:          len(extractor.AllPanels(dash)),
			TotalTargets:         totalTargets,
			ParseErrors:          len(parseErrors),
			AnalyzerVersion:      "0.2.0",
			CardinalityAvailable: cardData != nil,
			QueryCosts:           queryCosts,
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

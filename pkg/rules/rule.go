package rules

import (
	"fmt"
	"math"

	"github.com/dashboard-advisor/pkg/cardinality"
	"github.com/dashboard-advisor/pkg/extractor"
	"github.com/prometheus/prometheus/promql/parser"
)

// Severity levels for findings, ordered from least to most severe.
type Severity int

const (
	Low      Severity = iota // weight: 2
	Medium                   // weight: 5
	High                     // weight: 10
	Critical                 // weight: 15
)

// SeverityWeight returns the scoring weight for a severity level.
func SeverityWeight(s Severity) int {
	switch s {
	case Critical:
		return 15
	case High:
		return 10
	case Medium:
		return 5
	case Low:
		return 2
	default:
		return 0
	}
}

func (s Severity) String() string {
	switch s {
	case Low:
		return "Low"
	case Medium:
		return "Medium"
	case High:
		return "High"
	case Critical:
		return "Critical"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Finding represents a single detected issue in a dashboard.
type Finding struct {
	RuleID      string   // "Q1", "D2", "B1", etc. — stable, never renumbered
	Severity    Severity // Critical, High, Medium, Low
	PanelIDs    []int    // affected panel IDs (empty for dashboard-level findings)
	PanelTitles []string // human-readable panel names
	Title       string   // short: "Missing label filters"
	Why         string   // explanation of why this is a problem
	Fix         string   // what to change
	Impact      string   // expected improvement
	Validate    string   // how to verify the fix worked
	AutoFixable bool     // true if --fix can patch this automatically
	Confidence  float64  // 0.0-1.0; lower for static-only, higher with cardinality data
}

// Report is the output of analyzing one dashboard.
type Report struct {
	DashboardUID   string
	DashboardTitle string
	Score          int            // 0-100 composite health score
	Findings       []Finding
	PanelScores    map[int]int    // panel ID → per-panel score
	Metadata       ReportMetadata
}

// ReportMetadata holds supplementary info about the analysis run.
type ReportMetadata struct {
	TotalPanels          int
	TotalTargets         int
	ParseErrors          int
	AnalyzerVersion      string
	CardinalityAvailable bool               `json:"cardinalityAvailable"` // true if TSDB status was fetched
	QueryCosts           map[string]float64  `json:"queryCosts,omitempty"` // expr → estimated cost
}

// Rule is the interface every detection rule implements.
type Rule interface {
	ID() string
	RuleSeverity() Severity
	Check(ctx *AnalysisContext) []Finding
}

// AnalysisContext carries all data a rule might need.
type AnalysisContext struct {
	Dashboard   *extractor.DashboardModel
	Panels      []extractor.PanelModel            // all panels (including nested)
	Variables   []extractor.VariableModel          // template variables
	ParsedExprs map[string]parser.Expr             // raw expr → parsed AST
	Cardinality *cardinality.CardinalityData       // nil when no Prometheus URL provided (Phase 2)
	PrometheusURL string                           // empty when not configured; used by B-series rules
}

// ComputeScore calculates the composite health score from findings using
// an asymptotic formula that ensures every fix visibly improves the score.
//
//	score = round(100 × k / (penalty + k))
//
// where penalty = Σ(severity_weight) and k is a tuning constant (100).
// Properties:
//   - 0 penalty → 100 (perfect)
//   - penalty = k → 50 (midpoint: ~10 High findings or ~7 Critical)
//   - Score approaches 0 but never reaches it — every fix always moves the needle
//   - No clamping needed; the formula naturally stays in (0, 100]
func ComputeScore(findings []Finding) int {
	penalty := 0
	for _, f := range findings {
		penalty += SeverityWeight(f.Severity)
	}
	if penalty == 0 {
		return 100
	}
	const k = 100.0
	score := int(math.Round(100.0 * k / (float64(penalty) + k)))
	return score
}

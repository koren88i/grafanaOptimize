package rules_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dashboard-advisor/pkg/analyzer"
	"github.com/dashboard-advisor/pkg/extractor"
	"github.com/dashboard-advisor/pkg/rules"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "demo", "dashboards", name)
}

// buildContext loads a dashboard and builds an AnalysisContext with parsed exprs.
func buildContext(t *testing.T, name string) *rules.AnalysisContext {
	t.Helper()
	dash, err := extractor.LoadDashboard(testdataPath(name))
	if err != nil {
		t.Fatalf("failed to load %s: %v", name, err)
	}
	exprs := extractor.AllTargetExprs(dash)
	parsed, _ := analyzer.ParseAllExprs(exprs)
	return &rules.AnalysisContext{
		Dashboard:   dash,
		Panels:      extractor.PanelsWithTargets(dash),
		Variables:   dash.Templating.List,
		ParsedExprs: parsed,
	}
}

// --- Q1: Missing label filters ---

func TestQ1_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.MissingFilters{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q1 should detect missing label filters in slow dashboard")
	}
	t.Logf("Q1 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v: %s — %s", f.Severity, f.PanelIDs, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q1" {
			t.Errorf("finding has RuleID %q, want Q1", f.RuleID)
		}
		if f.Severity != rules.Critical {
			t.Errorf("finding has severity %s, want Critical", f.Severity)
		}
	}
}

func TestQ1_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.MissingFilters{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q1 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q3: Regex as equality ---

func TestQ3_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.RegexEquality{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q3 should detect regex-as-equality in slow dashboard")
	}
	t.Logf("Q3 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v: %s — %s", f.Severity, f.PanelIDs, f.Title, f.Fix)
	}

	for _, f := range findings {
		if !f.AutoFixable {
			t.Error("Q3 findings should be auto-fixable")
		}
	}
}

func TestQ3_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.RegexEquality{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q3 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Fix)
		}
	}
}

// --- D1: Too many panels ---

func TestD1_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.TooManyPanels{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D1 should detect too many panels in slow dashboard")
	}
	t.Logf("D1 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}
}

func TestD1_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.TooManyPanels{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D1 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- Combined: score check ---

func TestCombinedScore_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	allRules := []rules.Rule{
		&rules.MissingFilters{},
		&rules.RegexEquality{},
		&rules.TooManyPanels{},
	}

	var allFindings []rules.Finding
	for _, r := range allRules {
		allFindings = append(allFindings, r.Check(ctx)...)
	}

	score := rules.ComputeScore(allFindings)
	t.Logf("slow dashboard: %d findings, score = %d", len(allFindings), score)

	if score >= 100 {
		t.Errorf("slow dashboard score = %d, expected < 100", score)
	}
}

func TestCombinedScore_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	allRules := []rules.Rule{
		&rules.MissingFilters{},
		&rules.RegexEquality{},
		&rules.TooManyPanels{},
	}

	var allFindings []rules.Finding
	for _, r := range allRules {
		allFindings = append(allFindings, r.Check(ctx)...)
	}

	score := rules.ComputeScore(allFindings)
	t.Logf("fixed dashboard: %d findings, score = %d", len(allFindings), score)

	if score != 100 {
		t.Errorf("fixed dashboard score = %d, expected 100", score)
		for _, f := range allFindings {
			t.Logf("  [%s] %s: %s", f.RuleID, f.Title, f.Why)
		}
	}
}

// --- Q2: Unbounded regex ---

func TestQ2_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.UnboundedRegex{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q2 should detect unbounded regex in slow dashboard")
	}
	t.Logf("Q2 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v: %s — %s", f.Severity, f.PanelIDs, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q2" {
			t.Errorf("finding has RuleID %q, want Q2", f.RuleID)
		}
	}
}

func TestQ2_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.UnboundedRegex{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q2 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q4: High-cardinality grouping ---

func TestQ4_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.HighCardinalityGrouping{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q4 should detect high-cardinality grouping in slow dashboard")
	}
	t.Logf("Q4 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v: %s — %s", f.Severity, f.PanelIDs, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q4" {
			t.Errorf("finding has RuleID %q, want Q4", f.RuleID)
		}
	}
}

func TestQ4_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.HighCardinalityGrouping{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q4 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q5: Late aggregation ---

func TestQ5_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.LateAggregation{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q5 should detect late aggregation in slow dashboard")
	}
	t.Logf("Q5 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q5" {
			t.Errorf("finding has RuleID %q, want Q5", f.RuleID)
		}
	}
}

func TestQ5_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.LateAggregation{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q5 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q6: Long rate range ---

func TestQ6_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.LongRateRange{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q6 should detect long rate range in slow dashboard")
	}
	t.Logf("Q6 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q6" {
			t.Errorf("finding has RuleID %q, want Q6", f.RuleID)
		}
	}
}

func TestQ6_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.LongRateRange{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q6 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q7: Hardcoded interval ---

func TestQ7_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.HardcodedInterval{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q7 should detect hardcoded intervals in slow dashboard")
	}
	t.Logf("Q7 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q7" {
			t.Errorf("finding has RuleID %q, want Q7", f.RuleID)
		}
		if !f.AutoFixable {
			t.Error("Q7 findings should be auto-fixable")
		}
	}
}

func TestQ7_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.HardcodedInterval{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q7 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q8: Subquery abuse ---

func TestQ8_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.SubqueryAbuse{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q8 should detect subquery abuse in slow dashboard")
	}
	t.Logf("Q8 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q8" {
			t.Errorf("finding has RuleID %q, want Q8", f.RuleID)
		}
	}
}

func TestQ8_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.SubqueryAbuse{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q8 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q9: Duplicate expressions ---

func TestQ9_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.DuplicateExpressions{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q9 should detect duplicate expressions in slow dashboard")
	}
	t.Logf("Q9 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panels %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q9" {
			t.Errorf("finding has RuleID %q, want Q9", f.RuleID)
		}
	}
}

func TestQ9_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.DuplicateExpressions{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q9 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panels %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- Q10: Incorrect aggregation ---

func TestQ10_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.IncorrectAggregation{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("Q10 should detect incorrect aggregation in slow dashboard")
	}
	t.Logf("Q10 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "Q10" {
			t.Errorf("finding has RuleID %q, want Q10", f.RuleID)
		}
	}
}

func TestQ10_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.IncorrectAggregation{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("Q10 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- D2: Repeat with All ---

func TestD2_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.RepeatWithAll{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D2 should detect repeat-with-all in slow dashboard")
	}
	t.Logf("D2 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D2" {
			t.Errorf("finding has RuleID %q, want D2", f.RuleID)
		}
	}
}

func TestD2_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.RepeatWithAll{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D2 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- D3: Variable explosion ---

func TestD3_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.VariableExplosion{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D3 should detect variable explosion in slow dashboard")
	}
	t.Logf("D3 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D3" {
			t.Errorf("finding has RuleID %q, want D3", f.RuleID)
		}
	}
}

func TestD3_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.VariableExplosion{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D3 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- D4: Expensive variable query ---

func TestD4_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.ExpensiveVariableQuery{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D4 should detect expensive variable queries in slow dashboard")
	}
	t.Logf("D4 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D4" {
			t.Errorf("finding has RuleID %q, want D4", f.RuleID)
		}
	}
}

func TestD4_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.ExpensiveVariableQuery{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D4 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- D5: Refresh too frequent ---

func TestD5_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.RefreshTooFrequent{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D5 should detect too-frequent refresh in slow dashboard")
	}
	t.Logf("D5 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D5" {
			t.Errorf("finding has RuleID %q, want D5", f.RuleID)
		}
	}
}

func TestD5_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.RefreshTooFrequent{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D5 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- D6: Range too wide ---

func TestD6_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.RangeTooWide{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D6 should detect too-wide range in slow dashboard")
	}
	t.Logf("D6 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D6" {
			t.Errorf("finding has RuleID %q, want D6", f.RuleID)
		}
	}
}

func TestD6_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.RangeTooWide{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D6 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- D7: Missing maxDataPoints ---

func TestD7_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.MissingMaxDataPoints{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D7 should detect missing maxDataPoints in slow dashboard")
	}
	t.Logf("D7 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panel %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D7" {
			t.Errorf("finding has RuleID %q, want D7", f.RuleID)
		}
	}
}

func TestD7_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.MissingMaxDataPoints{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D7 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panel %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- D8: Duplicate queries ---

func TestD8_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.DuplicateQueries{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D8 should detect duplicate queries in slow dashboard")
	}
	t.Logf("D8 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] panels %v (%s): %s — %s", f.Severity, f.PanelIDs, f.PanelTitles, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D8" {
			t.Errorf("finding has RuleID %q, want D8", f.RuleID)
		}
	}
}

func TestD8_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.DuplicateQueries{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D8 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  panels %v: %s", f.PanelIDs, f.Why)
		}
	}
}

// --- D9: Datasource mixing ---

func TestD9_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.DatasourceMixing{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D9 should detect datasource mixing in slow dashboard")
	}
	t.Logf("D9 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D9" {
			t.Errorf("finding has RuleID %q, want D9", f.RuleID)
		}
	}
}

func TestD9_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.DatasourceMixing{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D9 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

// --- D10: No collapsed rows ---

func TestD10_SlowDashboard(t *testing.T) {
	ctx := buildContext(t, "slow-by-design.json")
	rule := &rules.NoCollapsedRows{}
	findings := rule.Check(ctx)

	if len(findings) == 0 {
		t.Fatal("D10 should detect no collapsed rows in slow dashboard")
	}
	t.Logf("D10 found %d findings:", len(findings))
	for _, f := range findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.Title, f.Why)
	}

	for _, f := range findings {
		if f.RuleID != "D10" {
			t.Errorf("finding has RuleID %q, want D10", f.RuleID)
		}
	}
}

func TestD10_FixedDashboard(t *testing.T) {
	ctx := buildContext(t, "fixed-by-advisor.json")
	rule := &rules.NoCollapsedRows{}
	findings := rule.Check(ctx)

	if len(findings) > 0 {
		t.Errorf("D10 should find no issues in fixed dashboard, got %d:", len(findings))
		for _, f := range findings {
			t.Logf("  %s", f.Why)
		}
	}
}

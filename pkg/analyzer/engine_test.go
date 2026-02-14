package analyzer

import (
	"testing"
)

func TestAnalyzeSlowDashboard(t *testing.T) {
	engine := DefaultEngine()
	report, err := engine.AnalyzeFile(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}

	t.Logf("Dashboard: %s (%s)", report.DashboardTitle, report.DashboardUID)
	t.Logf("Score: %d/100", report.Score)
	t.Logf("Findings: %d total", len(report.Findings))
	t.Logf("Metadata: %d panels, %d targets, %d parse errors",
		report.Metadata.TotalPanels, report.Metadata.TotalTargets, report.Metadata.ParseErrors)

	// Count by rule
	ruleCounts := map[string]int{}
	for _, f := range report.Findings {
		ruleCounts[f.RuleID]++
	}
	for id, count := range ruleCounts {
		t.Logf("  %s: %d findings", id, count)
	}

	if report.Score >= 70 {
		t.Errorf("slow dashboard score = %d, expected < 70", report.Score)
	}

	if len(report.Findings) == 0 {
		t.Error("expected findings on slow dashboard")
	}

	if report.DashboardUID != "slow-by-design" {
		t.Errorf("UID = %q, want %q", report.DashboardUID, "slow-by-design")
	}
}

func TestAnalyzeFixedDashboard(t *testing.T) {
	engine := DefaultEngine()
	report, err := engine.AnalyzeFile(testdataPath("fixed-by-advisor.json"))
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}

	t.Logf("Dashboard: %s (%s)", report.DashboardTitle, report.DashboardUID)
	t.Logf("Score: %d/100", report.Score)
	t.Logf("Findings: %d total", len(report.Findings))

	if report.Score != 100 {
		t.Errorf("fixed dashboard score = %d, expected 100", report.Score)
		for _, f := range report.Findings {
			t.Logf("  unexpected: [%s] %s — %s", f.RuleID, f.Title, f.Why)
		}
	}
}

func TestAnalyzePanelScores(t *testing.T) {
	engine := DefaultEngine()
	report, err := engine.AnalyzeFile(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("analysis failed: %v", err)
	}

	if len(report.PanelScores) == 0 {
		t.Error("expected per-panel scores on slow dashboard")
	}

	t.Logf("Per-panel scores:")
	for pid, score := range report.PanelScores {
		t.Logf("  panel %d: %d/100", pid, score)
	}
}

func TestAnalyzeNonexistentFile(t *testing.T) {
	engine := DefaultEngine()
	_, err := engine.AnalyzeFile("/nonexistent/dashboard.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

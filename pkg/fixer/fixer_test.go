package fixer

import (
	"os"
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

func TestApplyFixesReducesFindings(t *testing.T) {
	// Load slow dashboard
	path := testdataPath("slow-by-design.json")
	rawJSON, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read dashboard: %v", err)
	}

	// Analyze original
	engine := analyzer.DefaultEngine()
	dash, err := extractor.ParseDashboard(rawJSON)
	if err != nil {
		t.Fatalf("failed to parse dashboard: %v", err)
	}
	originalReport := engine.AnalyzeDashboard(dash)
	t.Logf("original: score=%d findings=%d", originalReport.Score, len(originalReport.Findings))

	// Count auto-fixable findings
	autoFixable := 0
	for _, f := range originalReport.Findings {
		if f.AutoFixable {
			autoFixable++
		}
	}
	t.Logf("auto-fixable findings: %d", autoFixable)

	if autoFixable == 0 {
		t.Fatal("expected auto-fixable findings on slow dashboard")
	}

	// Apply fixes
	patchedJSON, fixCount, err := ApplyFixes(rawJSON, originalReport.Findings)
	if err != nil {
		t.Fatalf("ApplyFixes failed: %v", err)
	}
	t.Logf("applied %d fixes", fixCount)

	if fixCount == 0 {
		t.Error("expected at least one fix to be applied")
	}

	// Re-analyze patched dashboard
	patchedDash, err := extractor.ParseDashboard(patchedJSON)
	if err != nil {
		t.Fatalf("patched JSON is invalid: %v", err)
	}
	patchedReport := engine.AnalyzeDashboard(patchedDash)
	t.Logf("patched: score=%d findings=%d", patchedReport.Score, len(patchedReport.Findings))

	// Finding count should decrease (score may clamp to 0 for both if many Critical findings remain)
	if len(patchedReport.Findings) >= len(originalReport.Findings) {
		t.Errorf("patched findings (%d) should be fewer than original (%d)",
			len(patchedReport.Findings), len(originalReport.Findings))
	}

	// The specific auto-fixable findings should be gone
	patchedAutoFixable := 0
	for _, f := range patchedReport.Findings {
		if f.AutoFixable {
			patchedAutoFixable++
		}
	}
	t.Logf("remaining auto-fixable: %d (was %d)", patchedAutoFixable, autoFixable)

	if patchedAutoFixable >= autoFixable {
		t.Error("expected fewer auto-fixable findings after applying fixes")
	}
}

func TestFixD5_SetsRefreshTo1m(t *testing.T) {
	rawJSON, err := os.ReadFile(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	findings := []rules.Finding{
		{RuleID: "D5", AutoFixable: true},
	}
	patchedJSON, count, err := ApplyFixes(rawJSON, findings)
	if err != nil {
		t.Fatalf("ApplyFixes failed: %v", err)
	}
	if count != 1 {
		t.Errorf("fix count = %d, want 1", count)
	}

	dash, _ := extractor.ParseDashboard(patchedJSON)
	if dash.Refresh != "1m" {
		t.Errorf("refresh = %q, want %q", dash.Refresh, "1m")
	}
}

func TestFixD6_SetsRangeTo1h(t *testing.T) {
	rawJSON, err := os.ReadFile(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	findings := []rules.Finding{
		{RuleID: "D6", AutoFixable: true},
	}
	patchedJSON, count, err := ApplyFixes(rawJSON, findings)
	if err != nil {
		t.Fatalf("ApplyFixes failed: %v", err)
	}
	if count != 1 {
		t.Errorf("fix count = %d, want 1", count)
	}

	dash, _ := extractor.ParseDashboard(patchedJSON)
	if dash.Time.From != "now-1h" {
		t.Errorf("time.from = %q, want %q", dash.Time.From, "now-1h")
	}
}

func TestFixQ3_ReplacesRegexWithEquality(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`http_requests_total{status=~"200"}`, `http_requests_total{status="200"}`},
		{`up{job=~"api"}`, `up{job="api"}`},
		{`up{status=~"5.."}`, `up{status=~"5.."}`}, // contains regex meta, should NOT change
		{`up{status=~".*error.*"}`, `up{status=~".*error.*"}`}, // contains regex meta
	}

	for _, tt := range tests {
		got := fixRegexEquality(tt.input)
		if got != tt.want {
			t.Errorf("fixRegexEquality(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
		}
	}
}

func TestPatchedJSONIsValid(t *testing.T) {
	rawJSON, err := os.ReadFile(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	engine := analyzer.DefaultEngine()
	dash, _ := extractor.ParseDashboard(rawJSON)
	report := engine.AnalyzeDashboard(dash)

	patchedJSON, _, err := ApplyFixes(rawJSON, report.Findings)
	if err != nil {
		t.Fatalf("ApplyFixes failed: %v", err)
	}

	// Verify patched JSON is valid by parsing it
	_, err = extractor.ParseDashboard(patchedJSON)
	if err != nil {
		t.Fatalf("patched JSON is invalid: %v", err)
	}
}

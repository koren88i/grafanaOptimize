package analyzer

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dashboard-advisor/pkg/extractor"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "demo", "dashboards", name)
}

func TestParseAllExprsFromSlowDashboard(t *testing.T) {
	dash, err := extractor.LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load dashboard: %v", err)
	}

	exprs := extractor.AllTargetExprs(dash)
	parsed, parseErrors := ParseAllExprs(exprs)

	t.Logf("parsed %d/%d expressions successfully", len(parsed), len(exprs))

	// The Q10 expression rate(sum(http_requests_total)[5m]) is intentionally
	// invalid PromQL — "ranges only allowed for vector selectors". This is the
	// anti-pattern we want to detect. It's OK that it doesn't parse.
	if len(parseErrors) > 1 {
		t.Errorf("expected at most 1 parse error (Q10), got %d:", len(parseErrors))
		for _, pe := range parseErrors {
			t.Logf("  %q — %v", pe.RawExpr, pe.ParseErr)
		}
	}

	if len(parsed) == 0 {
		t.Fatal("no expressions parsed successfully")
	}

	// Should parse all except the intentionally broken Q10 expression
	wantParsed := len(exprs) - 1
	if len(parsed) < wantParsed {
		t.Errorf("parsed %d expressions, want at least %d", len(parsed), wantParsed)
	}
}

func TestParseAllExprsFromFixedDashboard(t *testing.T) {
	dash, err := extractor.LoadDashboard(testdataPath("fixed-by-advisor.json"))
	if err != nil {
		t.Fatalf("failed to load dashboard: %v", err)
	}

	exprs := extractor.AllTargetExprs(dash)
	parsed, parseErrors := ParseAllExprs(exprs)

	t.Logf("parsed %d/%d expressions successfully", len(parsed), len(exprs))

	// With template variable replacement, all fixed dashboard expressions
	// should parse. $__rate_interval → 5m, $namespace → placeholder, etc.
	if len(parseErrors) > 0 {
		for _, pe := range parseErrors {
			t.Errorf("parse error: %q — %v", pe.RawExpr, pe.ParseErr)
		}
	}

	if len(parsed) != len(exprs) {
		t.Errorf("parsed %d expressions, want %d (all)", len(parsed), len(exprs))
	}
}

func TestParseAllExprsHandlesBrokenPromQL(t *testing.T) {
	exprs := []string{
		`rate(http_requests_total[5m])`,  // valid
		`sum(rate(`,                       // broken
		``,                                // empty (skipped)
		`sum by(job) (up{job="api"})`,     // valid
		`this is not promql {{{}`,         // broken
	}

	parsed, parseErrors := ParseAllExprs(exprs)

	if len(parsed) != 2 {
		t.Errorf("parsed %d expressions, want 2 valid", len(parsed))
	}

	if len(parseErrors) != 2 {
		t.Errorf("got %d parse errors, want 2", len(parseErrors))
	}

	if _, ok := parsed[`rate(http_requests_total[5m])`]; !ok {
		t.Error("missing parsed result for valid expression")
	}
	if _, ok := parsed[`sum by(job) (up{job="api"})`]; !ok {
		t.Error("missing parsed result for valid expression")
	}
}

func TestParseAllExprsEmpty(t *testing.T) {
	parsed, parseErrors := ParseAllExprs(nil)
	if len(parsed) != 0 {
		t.Errorf("parsed %d, want 0", len(parsed))
	}
	if len(parseErrors) != 0 {
		t.Errorf("errors %d, want 0", len(parseErrors))
	}
}

func TestReplaceTemplateVars(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			"rate_interval",
			`rate(http_requests_total[$__rate_interval])`,
			`rate(http_requests_total[5m])`,
		},
		{
			"interval",
			`rate(http_requests_total[$__interval])`,
			`rate(http_requests_total[5m])`,
		},
		{
			"dollar_var",
			`up{namespace="$namespace"}`,
			`up{namespace="placeholder"}`,
		},
		{
			"braced_var",
			`up{namespace="${namespace}"}`,
			`up{namespace="placeholder"}`,
		},
		{
			"no_vars",
			`rate(http_requests_total[5m])`,
			`rate(http_requests_total[5m])`,
		},
		{
			"multiple_vars",
			`up{job="$job", namespace="$namespace"}`,
			`up{job="placeholder", namespace="placeholder"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceTemplateVars(tt.expr)
			if got != tt.want {
				t.Errorf("ReplaceTemplateVars(%q)\n  got  %q\n  want %q", tt.expr, got, tt.want)
			}
		})
	}
}

package extractor

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	// Navigate from pkg/extractor/ to project root, then into demo/dashboards/
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "demo", "dashboards", name)
}

func TestLoadSlowDashboard(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load slow dashboard: %v", err)
	}

	if dash.UID != "slow-by-design" {
		t.Errorf("UID = %q, want %q", dash.UID, "slow-by-design")
	}
	if dash.Title == "" {
		t.Error("Title is empty")
	}

	// D5: refresh should be "10s"
	if dash.Refresh != "10s" {
		t.Errorf("Refresh = %q, want %q", dash.Refresh, "10s")
	}

	// D6: time range should be "now-7d"
	if dash.Time.From != "now-7d" {
		t.Errorf("Time.From = %q, want %q", dash.Time.From, "now-7d")
	}
}

func TestLoadFixedDashboard(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("fixed-by-advisor.json"))
	if err != nil {
		t.Fatalf("failed to load fixed dashboard: %v", err)
	}

	if dash.UID != "fixed-by-advisor" {
		t.Errorf("UID = %q, want %q", dash.UID, "fixed-by-advisor")
	}

	// D5: refresh should be "1m"
	if dash.Refresh != "1m" {
		t.Errorf("Refresh = %q, want %q", dash.Refresh, "1m")
	}

	// D6: time range should be "now-1h"
	if dash.Time.From != "now-1h" {
		t.Errorf("Time.From = %q, want %q", dash.Time.From, "now-1h")
	}
}

func TestSlowDashboardPanelCount(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	visible := VisiblePanels(dash)
	if len(visible) <= 25 {
		t.Errorf("visible panel count = %d, want >25 (D1 trigger)", len(visible))
	}
	t.Logf("slow dashboard: %d visible panels", len(visible))
}

func TestFixedDashboardPanelCount(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("fixed-by-advisor.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	visible := VisiblePanels(dash)
	if len(visible) > 25 {
		t.Errorf("visible panel count = %d, want <=25 (D1 should not trigger)", len(visible))
	}
	t.Logf("fixed dashboard: %d visible panels", len(visible))
}

func TestSlowDashboardVariables(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	vars := dash.Templating.List
	if len(vars) == 0 {
		t.Fatal("no template variables found")
	}

	// Find instance and pod variables
	var instanceVar, podVar *VariableModel
	for i := range vars {
		switch vars[i].Name {
		case "instance":
			instanceVar = &vars[i]
		case "pod":
			podVar = &vars[i]
		}
	}

	if instanceVar == nil {
		t.Fatal("missing $instance variable")
	}
	// D4: instance variable should use full PromQL, not label_values()
	qs := instanceVar.QueryString()
	if qs == "" {
		t.Error("$instance variable has empty query")
	}
	t.Logf("$instance query: %s", qs)

	if podVar == nil {
		t.Fatal("missing $pod variable")
	}
	// D3: pod variable should have includeAll and multi
	if !podVar.IncludeAll {
		t.Error("$pod.IncludeAll = false, want true")
	}
	if !podVar.Multi {
		t.Error("$pod.Multi = false, want true")
	}
}

func TestSlowDashboardTargetExprs(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	exprs := AllTargetExprs(dash)
	if len(exprs) == 0 {
		t.Fatal("no target expressions found")
	}
	t.Logf("found %d unique expressions", len(exprs))

	// Spot-check key expressions exist
	wantExprs := []string{
		`sum(rate(http_requests_total[5m]))`,
		`sum(node_filesystem_avail_bytes)`,
		`rate(go_goroutines[5m])`,
	}
	exprSet := make(map[string]bool)
	for _, e := range exprs {
		exprSet[e] = true
	}
	for _, want := range wantExprs {
		if !exprSet[want] {
			t.Errorf("missing expected expression: %s", want)
		}
	}
}

func TestSlowDashboardDatasources(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	uids := AllDatasourceUIDs(dash)
	if len(uids) < 3 {
		t.Errorf("datasource UID count = %d, want >=3 (D9 trigger)", len(uids))
	}
	t.Logf("datasource UIDs: %v", uids)
}

func TestFixedDashboardDatasources(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("fixed-by-advisor.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	uids := AllDatasourceUIDs(dash)
	if len(uids) > 2 {
		t.Errorf("datasource UID count = %d, want <=2 (D9 should not trigger)", len(uids))
	}
	t.Logf("datasource UIDs: %v", uids)
}

func TestSlowDashboardRepeatPanel(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	var found bool
	for _, p := range AllPanels(dash) {
		if p.Repeat != "" {
			found = true
			t.Logf("found repeat panel: %q repeats on %q", p.Title, p.Repeat)
		}
	}
	if !found {
		t.Error("no panel with repeat set (D2 trigger)")
	}
}

func TestPanelsWithTargets(t *testing.T) {
	dash, err := LoadDashboard(testdataPath("slow-by-design.json"))
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	panels := PanelsWithTargets(dash)
	if len(panels) == 0 {
		t.Fatal("no panels with targets found")
	}
	t.Logf("%d panels have at least one target expression", len(panels))

	// Every returned panel should have at least one non-empty expr
	for _, p := range panels {
		hasExpr := false
		for _, tgt := range p.Targets {
			if tgt.Expr != "" {
				hasExpr = true
				break
			}
		}
		if !hasExpr {
			t.Errorf("panel %d %q returned by PanelsWithTargets but has no expression", p.ID, p.Title)
		}
	}
}

func TestVariableQueryString(t *testing.T) {
	tests := []struct {
		name  string
		query interface{}
		want  string
	}{
		{"string query", "label_values(up, instance)", "label_values(up, instance)"},
		{"nil query", nil, ""},
		{"object query", map[string]interface{}{"query": "count(up)", "refId": "A"}, "count(up)"},
		{"object without query key", map[string]interface{}{"refId": "A"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := VariableModel{Query: tt.query}
			got := v.QueryString()
			if got != tt.want {
				t.Errorf("QueryString() = %q, want %q", got, tt.want)
			}
		})
	}
}

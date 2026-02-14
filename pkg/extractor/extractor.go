package extractor

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadDashboard reads a Grafana dashboard JSON file and returns a DashboardModel.
func LoadDashboard(path string) (*DashboardModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading dashboard file: %w", err)
	}
	return ParseDashboard(data)
}

// ParseDashboard parses raw JSON bytes into a DashboardModel.
func ParseDashboard(data []byte) (*DashboardModel, error) {
	var dash DashboardModel
	if err := json.Unmarshal(data, &dash); err != nil {
		return nil, fmt.Errorf("parsing dashboard JSON: %w", err)
	}
	return &dash, nil
}

// AllPanels returns all panels in the dashboard, including panels nested
// inside collapsed rows. The row panels themselves are included.
func AllPanels(dash *DashboardModel) []PanelModel {
	var all []PanelModel
	for _, p := range dash.Panels {
		all = append(all, p)
		for _, nested := range p.NestedPanels {
			all = append(all, nested)
		}
	}
	return all
}

// VisiblePanels returns panels that fire queries on dashboard load.
// This excludes row-type panels and panels inside collapsed rows.
func VisiblePanels(dash *DashboardModel) []PanelModel {
	var visible []PanelModel
	for _, p := range dash.Panels {
		if p.Type == "row" {
			continue
		}
		visible = append(visible, p)
	}
	return visible
}

// PanelsWithTargets returns non-row panels that have at least one target
// with a non-empty expression. Includes nested panels from collapsed rows.
func PanelsWithTargets(dash *DashboardModel) []PanelModel {
	var result []PanelModel
	for _, p := range AllPanels(dash) {
		if p.Type == "row" {
			continue
		}
		for _, t := range p.Targets {
			if t.Expr != "" {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// AllTargetExprs returns all unique PromQL expressions across all panels.
func AllTargetExprs(dash *DashboardModel) []string {
	seen := make(map[string]bool)
	var exprs []string
	for _, p := range AllPanels(dash) {
		for _, t := range p.Targets {
			if t.Expr != "" && !seen[t.Expr] {
				seen[t.Expr] = true
				exprs = append(exprs, t.Expr)
			}
		}
	}
	return exprs
}

// AllDatasourceUIDs returns all distinct datasource UIDs used across panels.
// Excludes template variable references (UIDs starting with "$").
func AllDatasourceUIDs(dash *DashboardModel) []string {
	seen := make(map[string]bool)
	var uids []string
	for _, p := range AllPanels(dash) {
		if p.Datasource != nil && p.Datasource.UID != "" {
			uid := p.Datasource.UID
			if len(uid) > 0 && uid[0] == '$' {
				continue
			}
			if !seen[uid] {
				seen[uid] = true
				uids = append(uids, uid)
			}
		}
		for _, t := range p.Targets {
			if t.Datasource != nil && t.Datasource.UID != "" {
				uid := t.Datasource.UID
				if len(uid) > 0 && uid[0] == '$' {
					continue
				}
				if !seen[uid] {
					seen[uid] = true
					uids = append(uids, uid)
				}
			}
		}
	}
	return uids
}

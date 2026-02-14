package extractor

import "encoding/json"

// DashboardModel represents a parsed Grafana dashboard.
type DashboardModel struct {
	UID          string          `json:"uid"`
	Title        string          `json:"title"`
	Refresh      string          `json:"refresh"`
	SchemaVersion int            `json:"schemaVersion"`
	Time         TimeRange       `json:"time"`
	Panels       []PanelModel    `json:"panels"`
	Templating   TemplatingModel `json:"templating"`
}

type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type TemplatingModel struct {
	List []VariableModel `json:"list"`
}

// PanelModel represents a single panel extracted from dashboard JSON.
type PanelModel struct {
	ID              int               `json:"id"`
	Title           string            `json:"title"`
	Type            string            `json:"type"`
	Collapsed       bool              `json:"collapsed"`
	Repeat          string            `json:"repeat,omitempty"`
	RepeatDirection string            `json:"repeatDirection,omitempty"`
	MaxPerRow       int               `json:"maxPerRow,omitempty"`
	MaxDataPoints   *int              `json:"maxDataPoints,omitempty"`
	Interval        string            `json:"interval,omitempty"`
	Targets         []TargetModel     `json:"targets"`
	Datasource      *DatasourceRef    `json:"datasource,omitempty"`
	// NestedPanels holds panels inside collapsed rows.
	NestedPanels    []PanelModel      `json:"panels,omitempty"`
	GridPos         json.RawMessage   `json:"gridPos,omitempty"`
}

// TargetModel represents a single query target within a panel.
type TargetModel struct {
	Expr         string         `json:"expr"`
	LegendFormat string         `json:"legendFormat,omitempty"`
	Datasource   *DatasourceRef `json:"datasource,omitempty"`
	RefID        string         `json:"refId,omitempty"`
}

// DatasourceRef identifies a datasource.
type DatasourceRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

// VariableModel represents a template variable.
type VariableModel struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Label      string         `json:"label,omitempty"`
	Query      interface{}    `json:"query"` // can be string or object depending on datasource
	Refresh    int            `json:"refresh"`
	IncludeAll bool           `json:"includeAll"`
	Multi      bool           `json:"multi"`
	AllValue   string         `json:"allValue,omitempty"`
	Regex      string         `json:"regex,omitempty"`
	Sort       int            `json:"sort,omitempty"`
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	Hide       int            `json:"hide,omitempty"`
}

// QueryString returns the variable query as a string.
// Handles both string queries and object queries (e.g. {query: "...", refId: "..."}).
func (v *VariableModel) QueryString() string {
	if v.Query == nil {
		return ""
	}
	switch q := v.Query.(type) {
	case string:
		return q
	case map[string]interface{}:
		if qs, ok := q["query"].(string); ok {
			return qs
		}
	}
	return ""
}

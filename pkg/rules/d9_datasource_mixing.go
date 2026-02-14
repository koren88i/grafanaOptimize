package rules

import (
	"fmt"
	"strings"

	"github.com/dashboard-advisor/pkg/extractor"
)

// DatasourceMixing detects dashboards that query more than a threshold number
// of distinct datasources. Mixing many datasources on one dashboard increases
// complexity, slows loading (parallel connections to multiple backends), and
// makes the dashboard harder to maintain.
type DatasourceMixing struct {
	// MaxDatasources is the maximum number of distinct datasource UIDs
	// before flagging. Defaults to 2 if zero.
	MaxDatasources int
}

func (r *DatasourceMixing) ID() string            { return "D9" }
func (r *DatasourceMixing) RuleSeverity() Severity { return Low }

func (r *DatasourceMixing) maxDatasources() int {
	if r.MaxDatasources > 0 {
		return r.MaxDatasources
	}
	return 2
}

func (r *DatasourceMixing) Check(ctx *AnalysisContext) []Finding {
	uids := extractor.AllDatasourceUIDs(ctx.Dashboard)
	maxDS := r.maxDatasources()

	if len(uids) <= maxDS {
		return nil
	}

	return []Finding{
		{
			RuleID:   "D9",
			Severity: Low,
			Title:    "Too many distinct datasources",
			Why: fmt.Sprintf(
				"Dashboard uses %d distinct datasources [%s] (threshold: %d). "+
					"Each datasource requires a separate connection, increasing load time and complexity.",
				len(uids), strings.Join(uids, ", "), maxDS,
			),
			Fix:         "Split the dashboard by datasource, or consolidate queries to fewer backends.",
			Impact:      fmt.Sprintf("Reducing from %d to ≤%d datasources simplifies connection management and may reduce load time", len(uids), maxDS),
			Validate:    "Check dashboard settings and panel datasource configurations",
			AutoFixable: false,
			Confidence:  0.8,
		},
	}
}

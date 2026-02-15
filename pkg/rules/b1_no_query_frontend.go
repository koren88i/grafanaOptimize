package rules

import (
	"strings"

	"github.com/dashboard-advisor/pkg/extractor"
)

// NoQueryFrontend detects dashboards querying Thanos without a query-frontend.
// The query-frontend provides caching, query splitting, and retries that
// dramatically reduce query latency and backend load.
type NoQueryFrontend struct{}

func (r *NoQueryFrontend) ID() string            { return "B1" }
func (r *NoQueryFrontend) RuleSeverity() Severity { return Critical }

func (r *NoQueryFrontend) Check(ctx *AnalysisContext) []Finding {
	if !dashboardUsesThanos(ctx) {
		return nil
	}

	// Static inference: if we see Thanos datasources, there's likely no
	// query-frontend since we can't verify its presence without a live endpoint.
	confidence := 0.5

	// TODO: If PrometheusURL is set, probe for query-frontend by checking
	// response headers or querying thanos_query_frontend_queries_total metrics.
	// If confirmed present, return nil. If confirmed absent, confidence = 0.9.

	return []Finding{
		{
			RuleID:      "B1",
			Severity:    Critical,
			Title:       "No Thanos query-frontend detected",
			Why:         "Dashboard uses a Thanos datasource but no query-frontend is detected. Without it, every query hits the querier directly, missing caching, query splitting, and retry benefits.",
			Fix:         "Deploy a Thanos query-frontend in front of the querier. Configure response caching with memcached and enable query splitting (--query-range.split-interval=24h).",
			Impact:      "Query-frontend typically reduces p99 latency by 50-90% for repeated queries through caching and query splitting",
			Validate:    "Check that the Grafana datasource URL points to the query-frontend, not directly to the querier",
			AutoFixable: false,
			Confidence:  confidence,
		},
	}
}

// dashboardUsesThanos checks if any datasource in the dashboard appears to be
// a Thanos querier based on the datasource UID or type patterns.
func dashboardUsesThanos(ctx *AnalysisContext) bool {
	for _, p := range ctx.Panels {
		if isDatasourceThanos(p.Datasource) {
			return true
		}
		for _, t := range p.Targets {
			if isDatasourceThanos(t.Datasource) {
				return true
			}
		}
	}
	for _, v := range ctx.Variables {
		if isDatasourceThanos(v.Datasource) {
			return true
		}
	}
	return false
}

func isDatasourceThanos(ds *extractor.DatasourceRef) bool {
	if ds == nil {
		return false
	}
	uid := strings.ToLower(ds.UID)
	return strings.Contains(uid, "thanos")
}

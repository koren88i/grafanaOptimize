package analyzer

import (
	"math"
	"testing"

	"github.com/dashboard-advisor/pkg/cardinality"
	"github.com/prometheus/prometheus/promql/parser"
)

const costEpsilon = 0.01

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < costEpsilon
}

func mustParse(t *testing.T, expr string) parser.Expr {
	t.Helper()
	parsed, err := parser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", expr, err)
	}
	return parsed
}

func TestEstimateQueryCost_SimpleVector(t *testing.T) {
	expr := mustParse(t, `up`)
	cost := EstimateQueryCost(expr, nil, 15)
	// Without cardinality data, uses DefaultHeuristicSeries (1000)
	if !approxEqual(cost, 1000) {
		t.Errorf("simple vector cost = %f, want 1000", cost)
	}
}

func TestEstimateQueryCost_WithCardinality(t *testing.T) {
	card := &cardinality.CardinalityData{
		SeriesByMetric: map[string]int{"up": 50},
	}
	expr := mustParse(t, `up`)
	cost := EstimateQueryCost(expr, card, 15)
	if !approxEqual(cost, 50) {
		t.Errorf("vector with cardinality cost = %f, want 50", cost)
	}
}

func TestEstimateQueryCost_RateWithMatrix(t *testing.T) {
	expr := mustParse(t, `rate(http_requests_total[5m])`)
	cost := EstimateQueryCost(expr, nil, 15)
	// 1000 series × (300s / 15s) = 1000 × 20 = 20000, × rate factor 1.0
	expected := 20000.0
	if !approxEqual(cost, expected) {
		t.Errorf("rate(metric[5m]) cost = %f, want %f", cost, expected)
	}
}

func TestEstimateQueryCost_SumByRate(t *testing.T) {
	expr := mustParse(t, `sum by(job) (rate(http_requests_total[5m]))`)
	cost := EstimateQueryCost(expr, nil, 15)
	// Inner: 1000 × (300/15) = 20000
	// Agg factor: 1.0 + (0.2 × 0 depth) + (0.1 × 1 grouping label) = 1.1
	// Total: 20000 × 1.1 = 22000
	expected := 22000.0
	if !approxEqual(cost, expected) {
		t.Errorf("sum by(job)(rate(...[5m])) cost = %f, want %f", cost, expected)
	}
}

func TestEstimateQueryCost_HistogramQuantile(t *testing.T) {
	expr := mustParse(t, `histogram_quantile(0.99, sum by(le) (rate(http_request_duration_seconds_bucket[5m])))`)
	cost := EstimateQueryCost(expr, nil, 15)
	// Inner rate: 1000 × (300/15) = 20000
	// Sum agg factor: 1.0 + 0 + 0.1 = 1.1 → 22000
	// histogram_quantile factor: 2.0 → 22000 × 2.0 = 44000
	// Plus the first arg (0.99) costs 0
	expected := 44000.0
	if !approxEqual(cost, expected) {
		t.Errorf("histogram_quantile cost = %f, want %f", cost, expected)
	}
}

func TestEstimateQueryCost_NestedAggregation(t *testing.T) {
	expr := mustParse(t, `max by(instance) (sum by(instance, job) (rate(x[5m])))`)
	cost := EstimateQueryCost(expr, nil, 15)
	// rate(x[5m]): 1000 × 20 = 20000
	// inner sum (depth=1): factor = 1.0 + 0.2×0 + 0.1×2 = 1.2 → 24000
	// outer max (depth=0, but this is depth=0 since it's the top): factor = 1.0 + 0.2×0 + 0.1×1 = 1.1 → 26400
	// Wait — aggregation depth: outer max is depth 0, inner sum is depth 1
	// rate(x[5m]) → walkCost at depth=2 (called from sum which is depth=1, which calls walkCost with depth+1=2)
	// Actually: max calls walkCost(sum, depth+1=1). sum calls walkCost(rate, depth+1=2).
	// rate cost from its inner: matrix selector → walkCost at depth=2
	// rate(x[5m]): walkCost Call depth=2, child = matrix = 1000 × 20 = 20000, factor=1.0 → 20000
	// sum by(instance,job) depth=1: innerCost=20000, factor = 1.0 + 0.2×1 + 0.1×2 = 1.4 → 28000
	// max by(instance) depth=0: innerCost=28000, factor = 1.0 + 0.2×0 + 0.1×1 = 1.1 → 30800
	expected := 30800.0
	if !approxEqual(cost, expected) {
		t.Errorf("nested agg cost = %f, want %f", cost, expected)
	}
}

func TestEstimateQueryCost_Subquery(t *testing.T) {
	expr := mustParse(t, `avg_over_time(rate(x[5m])[1h:30s])`)
	cost := EstimateQueryCost(expr, nil, 15)
	// This parses as: Call(avg_over_time, SubqueryExpr(Call(rate, MatrixSelector)))
	// rate(x[5m]): matrix = 1000 × (300/15) = 20000, rate factor 1.0 → 20000
	// Subquery [1h:30s]: inner=20000, evaluations = 3600/30 = 120 → 2400000
	// avg_over_time factor: 1.5 → 2400000 × 1.5 = 3600000
	expected := 3600000.0
	if !approxEqual(cost, expected) {
		t.Errorf("subquery cost = %f, want %f", cost, expected)
	}
}

func TestEstimateQueryCost_BinaryExpr(t *testing.T) {
	expr := mustParse(t, `up + up`)
	cost := EstimateQueryCost(expr, nil, 15)
	// Each side: 1000, total: 2000
	if !approxEqual(cost, 2000) {
		t.Errorf("binary expr cost = %f, want 2000", cost)
	}
}

func TestEstimateQueryCost_NilExpr(t *testing.T) {
	cost := EstimateQueryCost(nil, nil, 15)
	if !approxEqual(cost, 0) {
		t.Errorf("nil expr cost = %f, want 0", cost)
	}
}

func TestEstimateQueryCost_NumberLiteral(t *testing.T) {
	expr := mustParse(t, `42`)
	cost := EstimateQueryCost(expr, nil, 15)
	if !approxEqual(cost, 0) {
		t.Errorf("number literal cost = %f, want 0", cost)
	}
}

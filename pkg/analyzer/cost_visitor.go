package analyzer

import (
	"github.com/dashboard-advisor/pkg/cardinality"
	"github.com/prometheus/prometheus/promql/parser"
)

// functionCosts maps PromQL function names to their relative cost multiplier.
// Unlisted functions default to 1.0.
var functionCosts = map[string]float64{
	"rate":                1.0,
	"irate":               1.0,
	"increase":            1.0,
	"delta":               1.0,
	"idelta":              1.0,
	"histogram_quantile":  2.0,
	"quantile_over_time":  2.0,
	"sort":                0.5,
	"sort_desc":           0.5,
	"label_replace":       0.3,
	"label_join":          0.3,
	"avg_over_time":       1.5,
	"sum_over_time":       1.5,
	"max_over_time":       1.5,
	"min_over_time":       1.5,
	"count_over_time":     1.5,
	"stddev_over_time":    1.5,
	"absent":              0.1,
	"absent_over_time":    0.1,
	"vector":              0.01,
	"scalar":              0.01,
	"time":                0.01,
}

// EstimateQueryCost walks a PromQL AST and returns a numeric cost estimate.
// Higher values indicate more expensive queries. The cost is relative, not
// absolute — it's useful for ranking queries against each other.
//
// Formula:
//
//	cost = Σ(selector_costs) × aggregation_factor × function_factor
//	selector_cost = estimated_series(metric) × (range_seconds / step_seconds)
//	aggregation_factor = 1.0 + (0.2 × nesting_depth) + (0.1 × len(grouping))
//	function_factor = base_cost(func_name)  [default 1.0]
func EstimateQueryCost(expr parser.Expr, card *cardinality.CardinalityData, stepSeconds float64) float64 {
	if expr == nil {
		return 0
	}
	if stepSeconds <= 0 {
		stepSeconds = 15 // sensible default
	}
	return walkCost(expr, card, stepSeconds, 0)
}

func walkCost(node parser.Node, card *cardinality.CardinalityData, stepSeconds float64, depth int) float64 {
	if node == nil {
		return 0
	}

	switch n := node.(type) {
	case *parser.VectorSelector:
		series := float64(card.EstimatedSeries(n.Name, cardinality.DefaultHeuristicSeries))
		return series

	case *parser.MatrixSelector:
		// Matrix selector: series × (range / step)
		inner := walkCost(n.VectorSelector, card, stepSeconds, depth)
		rangeSeconds := n.Range.Seconds()
		if rangeSeconds <= 0 {
			rangeSeconds = stepSeconds
		}
		return inner * (rangeSeconds / stepSeconds)

	case *parser.AggregateExpr:
		innerCost := walkCost(n.Expr, card, stepSeconds, depth+1)
		aggFactor := 1.0 + (0.2 * float64(depth)) + (0.1 * float64(len(n.Grouping)))
		return innerCost * aggFactor

	case *parser.Call:
		// Sum child costs and multiply by function factor
		var childCost float64
		for _, arg := range n.Args {
			childCost += walkCost(arg, card, stepSeconds, depth)
		}
		factor := functionCost(n.Func.Name)
		return childCost * factor

	case *parser.BinaryExpr:
		left := walkCost(n.LHS, card, stepSeconds, depth)
		right := walkCost(n.RHS, card, stepSeconds, depth)
		return left + right

	case *parser.ParenExpr:
		return walkCost(n.Expr, card, stepSeconds, depth)

	case *parser.SubqueryExpr:
		innerCost := walkCost(n.Expr, card, stepSeconds, depth)
		rangeSeconds := n.Range.Seconds()
		subStep := n.Step.Seconds()
		if subStep <= 0 {
			subStep = stepSeconds
		}
		evaluations := rangeSeconds / subStep
		if evaluations < 1 {
			evaluations = 1
		}
		return innerCost * evaluations

	case *parser.UnaryExpr:
		return walkCost(n.Expr, card, stepSeconds, depth)

	case *parser.StepInvariantExpr:
		return walkCost(n.Expr, card, stepSeconds, depth)

	case *parser.NumberLiteral, *parser.StringLiteral:
		return 0

	default:
		return 0
	}
}

func functionCost(name string) float64 {
	if cost, ok := functionCosts[name]; ok {
		return cost
	}
	return 1.0
}

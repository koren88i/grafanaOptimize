package rules

import (
	"fmt"
	"strings"
)

// defaultValuesPerVariable is the assumed cardinality of each multi-select
// variable with Include All enabled, used when actual cardinality is unknown.
const defaultValuesPerVariable = 100

// VariableExplosion detects dashboards where the cross-product of multi-select
// variables with Include All enabled exceeds a safe threshold. When multiple
// such variables exist, selecting All on each creates a combinatorial explosion
// of repeated panels or query permutations.
type VariableExplosion struct {
	// Threshold is the maximum allowed cross-product before flagging.
	// Defaults to 50 if zero.
	Threshold int
}

func (r *VariableExplosion) ID() string            { return "D3" }
func (r *VariableExplosion) RuleSeverity() Severity { return Critical }

func (r *VariableExplosion) threshold() int {
	if r.Threshold > 0 {
		return r.Threshold
	}
	return 50
}

func (r *VariableExplosion) Check(ctx *AnalysisContext) []Finding {
	// Collect variable names that are both multi-select and include-all.
	var explosiveVars []string
	for _, v := range ctx.Variables {
		if v.IncludeAll && v.Multi {
			explosiveVars = append(explosiveVars, v.Name)
		}
	}

	if len(explosiveVars) == 0 {
		return nil
	}

	// Estimate cross-product: each variable contributes defaultValuesPerVariable.
	product := 1
	for range explosiveVars {
		product *= defaultValuesPerVariable
		// Guard against integer overflow for many variables.
		if product > 1_000_000 {
			product = 1_000_000
			break
		}
	}

	thresh := r.threshold()
	if product <= thresh {
		return nil
	}

	return []Finding{
		{
			RuleID:   "D3",
			Severity: Critical,
			Title:    "Variable cross-product explosion",
			Why: fmt.Sprintf(
				"Variables [%s] are all multi-select with Include All. Estimated cross-product: %d (threshold: %d). "+
					"Selecting All on each creates a combinatorial explosion of query permutations.",
				strings.Join(explosiveVars, ", "), product, thresh,
			),
			Fix:         "Disable Include All or Multi on some variables, or add ad-hoc filters instead of multi-select variables.",
			Impact:      fmt.Sprintf("Reducing the cross-product from %d to ≤%d prevents combinatorial query fan-out", product, thresh),
			Validate:    "Select All on all flagged variables and verify query count in browser DevTools",
			AutoFixable: false,
			Confidence:  0.7,
		},
	}
}

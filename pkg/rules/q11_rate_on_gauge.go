package rules

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"
)

// knownGaugePrefixes lists metric name prefixes that are known to be gauge-type.
var knownGaugePrefixes = []string{
	"go_goroutines",
	"go_threads",
	"go_memstats_",
	"go_info",
	"process_resident_memory_bytes",
	"process_virtual_memory_bytes",
	"process_open_fds",
	"process_max_fds",
	"node_memory_",
	"node_filesystem_",
	"node_load",
	"node_time_seconds",
	"node_boot_time_seconds",
	"prometheus_tsdb_head_series",
	"prometheus_tsdb_head_chunks",
	"up",
}

// RateOnGauge detects rate() or irate() applied to gauge-type metrics.
// These functions compute per-second change and only make sense for counters
// (monotonically increasing values). Applying them to gauges produces
// meaningless results (often mostly zeros with occasional spikes).
type RateOnGauge struct{}

func (r *RateOnGauge) ID() string            { return "Q11" }
func (r *RateOnGauge) RuleSeverity() Severity { return Medium }

func (r *RateOnGauge) Check(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			expr, ok := ctx.ParsedExprs[target.Expr]
			if !ok {
				continue
			}
			parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
				call, ok := node.(*parser.Call)
				if !ok {
					return nil
				}
				if call.Func.Name != "rate" && call.Func.Name != "irate" {
					return nil
				}
				// rate/irate takes a matrix selector as first argument
				if len(call.Args) == 0 {
					return nil
				}
				metricName := extractMetricName(call.Args[0])
				if metricName == "" {
					return nil
				}
				if !isLikelyGauge(metricName) {
					return nil
				}
				findings = append(findings, Finding{
					RuleID:      "Q11",
					Severity:    Medium,
					PanelIDs:    []int{panel.ID},
					PanelTitles: []string{panel.Title},
					Title:       "rate()/irate() on gauge metric",
					Why:         fmt.Sprintf("%s() is applied to %q, which appears to be a gauge metric. rate/irate compute per-second change and only produce meaningful results on counters (_total, _count, _bucket).", call.Func.Name, metricName),
					Fix:         fmt.Sprintf("Use the metric directly (%s) or use delta() / deriv() instead of %s() for gauge metrics.", metricName, call.Func.Name),
					Impact:      "Correct function choice produces accurate visualizations instead of mostly-zero noise",
					Validate:    "Compare rate() output with raw metric — gauges should show actual values, not per-second derivatives",
					AutoFixable: false,
					Confidence:  0.6,
				})
				return nil
			})
		}
	}
	return findings
}

// isLikelyGauge returns true if the metric name matches known gauge patterns.
// Uses conservative matching: only flags metrics that are definitely gauges,
// not unknown metrics.
func isLikelyGauge(name string) bool {
	// Counters end in _total, _count, _sum, _bucket — these are NOT gauges
	if strings.HasSuffix(name, "_total") || strings.HasSuffix(name, "_count") ||
		strings.HasSuffix(name, "_sum") || strings.HasSuffix(name, "_bucket") {
		return false
	}
	for _, prefix := range knownGaugePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// extractMetricName extracts the metric name from a PromQL expression,
// handling both VectorSelector and MatrixSelector.
func extractMetricName(expr parser.Expr) string {
	var name string
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		vs, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		if vs.Name != "" {
			name = vs.Name
			return nil
		}
		for _, m := range vs.LabelMatchers {
			if m.Name == "__name__" {
				name = m.Value
				return nil
			}
		}
		return nil
	})
	return name
}

package rules

import (
	"fmt"
	"time"
)

// RangeTooWide detects dashboards with a default time range wider than a safe
// maximum. Wide ranges pull large amounts of data per query, increasing
// response times and memory usage on both the datasource and the browser.
type RangeTooWide struct {
	// MaxRange is the maximum acceptable default time range.
	// Defaults to 24h if zero.
	MaxRange time.Duration
}

func (r *RangeTooWide) ID() string            { return "D6" }
func (r *RangeTooWide) RuleSeverity() Severity { return Medium }

func (r *RangeTooWide) maxRange() time.Duration {
	if r.MaxRange > 0 {
		return r.MaxRange
	}
	return 24 * time.Hour
}

func (r *RangeTooWide) Check(ctx *AnalysisContext) []Finding {
	from := ctx.Dashboard.Time.From
	if from == "" {
		return nil
	}

	d, err := parseRelativeRange(from)
	if err != nil {
		return nil
	}

	maxD := r.maxRange()
	if d <= maxD {
		return nil
	}

	return []Finding{
		{
			RuleID:      "D6",
			Severity:    Medium,
			Title:       "Default time range too wide",
			Why:         fmt.Sprintf("Dashboard default time range is %q (%s). Ranges wider than %s pull large data volumes per query, increasing response times and memory usage.", from, d, maxD),
			Fix:         fmt.Sprintf("Set the default time range to %s or less (e.g., \"now-6h\" or \"now-1h\").", maxD),
			Impact:      fmt.Sprintf("Narrowing from %s to %s reduces data scanned per query proportionally", d, maxD),
			Validate:    "Open dashboard settings → Time Options → verify the From value",
			AutoFixable: true,
			Confidence:  1.0,
		},
	}
}

// parseRelativeRange extracts the duration from a Grafana relative time string
// like "now-7d", "now-6h", "now-30m". Returns the parsed duration.
func parseRelativeRange(from string) (time.Duration, error) {
	// Expected format: "now-<duration>"
	if len(from) < 5 || from[:4] != "now-" {
		return 0, fmt.Errorf("not a relative range: %q", from)
	}
	durationPart := from[4:]
	return parseGrafanaDuration(durationPart)
}

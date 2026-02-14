package rules

import (
	"fmt"
	"time"
)

// RefreshTooFrequent detects dashboards with an auto-refresh interval shorter
// than a safe minimum. Very frequent refreshes cause continuous query load on
// the backend even when the dashboard is idle in a browser tab.
type RefreshTooFrequent struct {
	// MinRefresh is the minimum acceptable refresh interval.
	// Defaults to 30s if zero.
	MinRefresh time.Duration
}

func (r *RefreshTooFrequent) ID() string            { return "D5" }
func (r *RefreshTooFrequent) RuleSeverity() Severity { return Medium }

func (r *RefreshTooFrequent) minRefresh() time.Duration {
	if r.MinRefresh > 0 {
		return r.MinRefresh
	}
	return 30 * time.Second
}

func (r *RefreshTooFrequent) Check(ctx *AnalysisContext) []Finding {
	raw := ctx.Dashboard.Refresh
	if raw == "" {
		return nil
	}

	d, err := parseGrafanaDuration(raw)
	if err != nil {
		return nil
	}

	minD := r.minRefresh()
	if d >= minD {
		return nil
	}

	return []Finding{
		{
			RuleID:      "D5",
			Severity:    Medium,
			Title:       "Auto-refresh interval too frequent",
			Why:         fmt.Sprintf("Dashboard refresh is set to %s. Intervals below %s cause continuous backend query load, especially when many users have the dashboard open.", raw, minD),
			Fix:         fmt.Sprintf("Set the dashboard refresh interval to %s or longer.", minD),
			Impact:      fmt.Sprintf("Changing refresh from %s to %s reduces query rate by %.0f%%", raw, minD, (1.0-float64(d)/float64(minD))*100),
			Validate:    "Open dashboard settings → verify the refresh interval is updated",
			AutoFixable: true,
			Confidence:  1.0,
		},
	}
}

// parseGrafanaDuration parses Grafana-style duration strings such as "5s",
// "1m", "1h", "7d", "1w". Go's time.ParseDuration does not handle "d" or "w".
func parseGrafanaDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration string")
	}

	// Try standard Go parsing first (handles s, ms, m, h, etc.)
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse manually for Grafana-specific suffixes.
	n := 0
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == 0 || i >= len(s) {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	suffix := s[i:]
	switch suffix {
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case "ms":
		return time.Duration(n) * time.Millisecond, nil
	default:
		return 0, fmt.Errorf("unknown duration suffix %q in %q", suffix, s)
	}
}

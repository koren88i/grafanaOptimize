package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dashboard-advisor/pkg/rules"
)

// TextFormatter renders a human-readable report.
type TextFormatter struct{}

func (f *TextFormatter) Format(w io.Writer, report *rules.Report) error {
	// Header
	fmt.Fprintf(w, "Dashboard: %s (%s)\n", report.DashboardTitle, report.DashboardUID)
	fmt.Fprintf(w, "Score:     %s\n", scoreBar(report.Score))
	fmt.Fprintf(w, "Panels:    %d  |  Targets: %d  |  Parse errors: %d\n",
		report.Metadata.TotalPanels, report.Metadata.TotalTargets, report.Metadata.ParseErrors)
	fmt.Fprintln(w, strings.Repeat("─", 70))

	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "No issues found. Dashboard looks healthy!")
		return nil
	}

	// Group findings by rule ID
	grouped := groupByRule(report.Findings)
	ruleIDs := sortedKeys(grouped)

	fmt.Fprintf(w, "Found %d issue(s):\n\n", len(report.Findings))

	for _, ruleID := range ruleIDs {
		findings := grouped[ruleID]
		first := findings[0]
		fmt.Fprintf(w, "  %s  %s [%s] (%d occurrence%s)\n",
			severityIcon(first.Severity), ruleID, first.Title,
			len(findings), plural(len(findings)))

		// Show affected panels
		panels := collectPanels(findings)
		if len(panels) > 0 {
			fmt.Fprintf(w, "       Panels: %s\n", panels)
		}
		fmt.Fprintf(w, "       Why:    %s\n", first.Why)
		fmt.Fprintf(w, "       Fix:    %s\n", first.Fix)
		fmt.Fprintf(w, "       Impact: %s\n", first.Impact)
		if first.AutoFixable {
			fmt.Fprintf(w, "       Auto-fixable: yes (use --fix)\n")
		}
		fmt.Fprintln(w)
	}

	return nil
}

func scoreBar(score int) string {
	label := "CRITICAL"
	if score >= 80 {
		label = "GOOD"
	} else if score >= 60 {
		label = "FAIR"
	} else if score >= 40 {
		label = "POOR"
	}
	filled := score / 5 // 20 chars max
	empty := 20 - filled
	return fmt.Sprintf("%d/100 [%s%s] %s",
		score, strings.Repeat("█", filled), strings.Repeat("░", empty), label)
}

func severityIcon(s rules.Severity) string {
	switch s {
	case rules.Critical:
		return "!!"
	case rules.High:
		return "! "
	case rules.Medium:
		return "~ "
	case rules.Low:
		return "- "
	default:
		return "  "
	}
}

func groupByRule(findings []rules.Finding) map[string][]rules.Finding {
	grouped := make(map[string][]rules.Finding)
	for _, f := range findings {
		grouped[f.RuleID] = append(grouped[f.RuleID], f)
	}
	return grouped
}

func sortedKeys(m map[string][]rules.Finding) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func collectPanels(findings []rules.Finding) string {
	seen := make(map[string]bool)
	var panels []string
	for _, f := range findings {
		for _, title := range f.PanelTitles {
			if title != "" && !seen[title] {
				seen[title] = true
				panels = append(panels, title)
			}
		}
	}
	if len(panels) > 5 {
		return fmt.Sprintf("%s, ... (+%d more)", strings.Join(panels[:5], ", "), len(panels)-5)
	}
	return strings.Join(panels, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

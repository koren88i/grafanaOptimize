package rules

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// DuplicateExpressions detects identical PromQL expressions used across
// multiple panels. Duplicate queries waste evaluation time — every copy
// is sent to Prometheus independently. They should be consolidated using
// shared queries, library panels, or recording rules.
type DuplicateExpressions struct{}

func (r *DuplicateExpressions) ID() string            { return "Q9" }
func (r *DuplicateExpressions) RuleSeverity() Severity { return High }

func (r *DuplicateExpressions) Check(ctx *AnalysisContext) []Finding {
	// Map normalized expression → list of panels that use it.
	type panelRef struct {
		ID    int
		Title string
	}
	exprPanels := make(map[string][]panelRef)

	for _, panel := range ctx.Panels {
		for _, target := range panel.Targets {
			normalized := normalizeExpr(target.Expr)
			if normalized == "" {
				continue
			}
			key := hashExpr(normalized)
			exprPanels[key] = append(exprPanels[key], panelRef{
				ID:    panel.ID,
				Title: panel.Title,
			})
		}
	}

	var findings []Finding
	for _, panels := range exprPanels {
		if len(panels) <= 2 {
			continue
		}
		// Deduplicate panel IDs (a panel might have the expr in multiple targets)
		seen := make(map[int]bool)
		var ids []int
		var titles []string
		for _, p := range panels {
			if !seen[p.ID] {
				seen[p.ID] = true
				ids = append(ids, p.ID)
				titles = append(titles, p.Title)
			}
		}
		if len(ids) <= 2 {
			continue
		}
		findings = append(findings, Finding{
			RuleID:      "Q9",
			Severity:    High,
			PanelIDs:    ids,
			PanelTitles: titles,
			Title:       "Duplicate expression across panels",
			Why:         fmt.Sprintf("The same PromQL expression is used in %d panels (%s). Each copy is evaluated independently, multiplying Prometheus load.", len(ids), strings.Join(titles, ", ")),
			Fix:         "Use a shared query (panel data source), a library panel, or a recording rule to evaluate the expression once.",
			Impact:      fmt.Sprintf("Eliminates %d redundant query evaluations per refresh", len(ids)-1),
			Validate:    "Verify each panel still renders after consolidation",
			AutoFixable: false,
			Confidence:  0.95,
		})
	}
	return findings
}

// normalizeExpr strips whitespace to normalize expressions for comparison.
func normalizeExpr(expr string) string {
	// Remove all whitespace for normalization
	var b strings.Builder
	for _, r := range expr {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hashExpr returns a hex-encoded SHA-256 of the expression string.
func hashExpr(expr string) string {
	h := sha256.Sum256([]byte(expr))
	return fmt.Sprintf("%x", h)
}

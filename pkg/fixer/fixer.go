package fixer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dashboard-advisor/pkg/rules"
)

// ApplyFixes takes raw dashboard JSON and a list of findings, applies
// auto-fixes for findings where AutoFixable is true, and returns the
// patched JSON. Non-auto-fixable findings are left unchanged.
func ApplyFixes(dashboardJSON []byte, findings []rules.Finding) ([]byte, int, error) {
	var dash map[string]interface{}
	if err := json.Unmarshal(dashboardJSON, &dash); err != nil {
		return nil, 0, fmt.Errorf("parsing dashboard JSON: %w", err)
	}

	fixCount := 0

	for _, f := range findings {
		if !f.AutoFixable {
			continue
		}
		var err error
		switch f.RuleID {
		case "Q3":
			dash, err = fixQ3(dash, f)
		case "Q7":
			dash, err = fixQ7(dash, f)
		case "D5":
			dash, err = fixD5(dash)
		case "D6":
			dash, err = fixD6(dash)
		case "D7":
			dash, err = fixD7(dash, f)
		default:
			continue
		}
		if err != nil {
			return nil, fixCount, fmt.Errorf("applying fix for %s: %w", f.RuleID, err)
		}
		fixCount++
	}

	patched, err := json.MarshalIndent(dash, "", "  ")
	if err != nil {
		return nil, fixCount, fmt.Errorf("marshaling patched JSON: %w", err)
	}
	return patched, fixCount, nil
}

// fixQ3 replaces =~"value" with ="value" for non-regex values in panel targets.
func fixQ3(dash map[string]interface{}, f rules.Finding) (map[string]interface{}, error) {
	panels, ok := dash["panels"].([]interface{})
	if !ok {
		return dash, nil
	}
	for _, p := range panels {
		panel, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		targets, ok := panel["targets"].([]interface{})
		if !ok {
			continue
		}
		for _, t := range targets {
			target, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			expr, ok := target["expr"].(string)
			if !ok {
				continue
			}
			// Replace =~"simplevalue" with ="simplevalue" for non-regex values
			target["expr"] = fixRegexEquality(expr)
		}
	}
	return dash, nil
}

var regexMatcherRe = regexp.MustCompile(`(=~)"([^"]*)"`)

func fixRegexEquality(expr string) string {
	return regexMatcherRe.ReplaceAllStringFunc(expr, func(match string) string {
		sub := regexMatcherRe.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		value := sub[2]
		if !containsRegexMeta(value) {
			return `="` + value + `"`
		}
		return match
	})
}

func containsRegexMeta(s string) bool {
	for _, c := range s {
		switch c {
		case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '\\':
			return true
		}
	}
	return false
}

// fixQ7 replaces hardcoded durations in rate/irate/increase with $__rate_interval.
func fixQ7(dash map[string]interface{}, f rules.Finding) (map[string]interface{}, error) {
	panels, ok := dash["panels"].([]interface{})
	if !ok {
		return dash, nil
	}
	for _, p := range panels {
		panel, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		fixTargetsQ7(panel)
		// Also fix nested panels in rows
		if nested, ok := panel["panels"].([]interface{}); ok {
			for _, np := range nested {
				if nestedPanel, ok := np.(map[string]interface{}); ok {
					fixTargetsQ7(nestedPanel)
				}
			}
		}
	}
	return dash, nil
}

var hardcodedIntervalRe = regexp.MustCompile(`((?:rate|irate|increase)\s*\([^[]*)\[(\d+[smhd])\]`)

func fixTargetsQ7(panel map[string]interface{}) {
	targets, ok := panel["targets"].([]interface{})
	if !ok {
		return
	}
	for _, t := range targets {
		target, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		expr, ok := target["expr"].(string)
		if !ok || strings.Contains(expr, "$__rate_interval") || strings.Contains(expr, "$__interval") {
			continue
		}
		// Use $$ to produce a literal $ in Go regex replacement
		target["expr"] = hardcodedIntervalRe.ReplaceAllString(expr, "${1}[$$__rate_interval]")
	}
}

// fixD5 sets refresh to "1m".
func fixD5(dash map[string]interface{}) (map[string]interface{}, error) {
	dash["refresh"] = "1m"
	return dash, nil
}

// fixD6 sets time.from to "now-1h".
func fixD6(dash map[string]interface{}) (map[string]interface{}, error) {
	timeMap, ok := dash["time"].(map[string]interface{})
	if !ok {
		timeMap = make(map[string]interface{})
		dash["time"] = timeMap
	}
	timeMap["from"] = "now-1h"
	return dash, nil
}

// fixD7 sets maxDataPoints on panels that are missing it.
func fixD7(dash map[string]interface{}, f rules.Finding) (map[string]interface{}, error) {
	panels, ok := dash["panels"].([]interface{})
	if !ok {
		return dash, nil
	}

	vizTypes := map[string]bool{
		"timeseries": true, "graph": true, "barchart": true, "heatmap": true,
	}

	for _, p := range panels {
		panel, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		pType, _ := panel["type"].(string)
		if vizTypes[pType] {
			if _, exists := panel["maxDataPoints"]; !exists {
				panel["maxDataPoints"] = 1000
			} else if mdp, ok := panel["maxDataPoints"].(float64); ok && mdp == 0 {
				panel["maxDataPoints"] = 1000
			}
		}
		// Also fix nested panels in rows
		if nested, ok := panel["panels"].([]interface{}); ok {
			for _, np := range nested {
				if nestedPanel, ok := np.(map[string]interface{}); ok {
					npType, _ := nestedPanel["type"].(string)
					if vizTypes[npType] {
						if _, exists := nestedPanel["maxDataPoints"]; !exists {
							nestedPanel["maxDataPoints"] = 1000
						}
					}
				}
			}
		}
	}
	return dash, nil
}

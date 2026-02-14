package analyzer

import (
	"log"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"
)

// ParseResult holds the outcome of parsing a single PromQL expression.
type ParseResult struct {
	Expr     parser.Expr
	RawExpr  string
	ParseErr error
}

// ParseAllExprs parses all PromQL expression strings into ASTs.
// Returns a map from raw expression string to parsed AST.
// Grafana template variables ($__rate_interval, $variable, etc.) are replaced
// with parseable placeholders before parsing.
// Unparseable expressions are logged and skipped — never crash.
func ParseAllExprs(exprs []string) (parsed map[string]parser.Expr, errors []ParseResult) {
	parsed = make(map[string]parser.Expr, len(exprs))
	for _, raw := range exprs {
		if raw == "" {
			continue
		}
		normalized := ReplaceTemplateVars(raw)
		expr, err := parser.ParseExpr(normalized)
		if err != nil {
			log.Printf("WARN: unparseable PromQL (skipped): %q — %v", raw, err)
			errors = append(errors, ParseResult{RawExpr: raw, ParseErr: err})
			continue
		}
		// Key by the original raw expression so rules can map back to panels
		parsed[raw] = expr
	}
	return parsed, errors
}

// ReplaceTemplateVars replaces Grafana template variables with parseable
// PromQL-compatible placeholders so the Prometheus parser can handle them.
//
// Duration variables ($__rate_interval, $__interval, $__range) → "5m"
// Label value variables ($variable) → "placeholder"
var grafanaDurationVars = []string{
	"$__rate_interval",
	"$__interval",
	"$__range",
	"${__rate_interval}",
	"${__interval}",
	"${__range}",
}

func ReplaceTemplateVars(expr string) string {
	result := expr

	// Replace Grafana duration variables with a parseable duration
	for _, v := range grafanaDurationVars {
		result = strings.ReplaceAll(result, v, "5m")
	}

	// Replace remaining $variable and ${variable} references in label matchers
	// with a placeholder string value
	result = replaceVariableRefs(result)

	return result
}

// replaceVariableRefs replaces $var and ${var} references with "placeholder".
// Only replaces in label value positions (inside quotes or as bare values).
func replaceVariableRefs(expr string) string {
	var b strings.Builder
	b.Grow(len(expr))
	i := 0
	for i < len(expr) {
		if expr[i] != '$' {
			b.WriteByte(expr[i])
			i++
			continue
		}

		// Found $, check what follows
		if i+1 >= len(expr) {
			b.WriteByte(expr[i])
			i++
			continue
		}

		if expr[i+1] == '{' {
			// ${var} form
			end := strings.IndexByte(expr[i:], '}')
			if end == -1 {
				b.WriteByte(expr[i])
				i++
				continue
			}
			b.WriteString("placeholder")
			i += end + 1
		} else if isIdentStart(expr[i+1]) {
			// $var form — consume identifier chars
			j := i + 1
			for j < len(expr) && isIdentChar(expr[j]) {
				j++
			}
			b.WriteString("placeholder")
			i = j
		} else {
			b.WriteByte(expr[i])
			i++
		}
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

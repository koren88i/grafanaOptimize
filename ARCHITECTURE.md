# Architecture Reference

This document is the technical reference for the Dashboard Performance Advisor. It contains the decisions, designs, and specifications that came out of the research phase. Consult this when you need to understand *why* something is built a certain way or *how* a specific component should work.

For the full research behind these decisions — including the competitor UX teardowns (Datadog DBM, New Relic, Splunk), all 15 OSS tools evaluated with scoring matrices, and the complete detection heuristic rationale with expected impact estimates — see [docs/RESEARCH.md](docs/RESEARCH.md).

---

## 1. Diagnosis model: three signal layers

The advisor collects signals at three progressively richer layers. Phase 1 implements only Layer 1. Each layer improves confidence and accuracy but requires more infrastructure.

**Layer 1 — Static analysis (dashboard JSON + PromQL AST).** Requires only the dashboard JSON (fetched via Grafana API or read from file). Parse JSON to extract panel count, query expressions, variable definitions (type, refresh policy, `includeAll`, `multi`, query expression), repeat configuration, refresh interval, time range, maxDataPoints, interval, collapsed row membership. Parse each PromQL expression into an AST using the Prometheus parser. Apply pattern-matching rules via `parser.Walk()` custom Visitors against the AST and structural checks against the JSON. This layer is deterministic, fast (<100ms per dashboard), and works completely offline. It is the entire MVP.

**Layer 2 — Live cardinality enrichment (Phase 2).** Query the Prometheus TSDB status API (`/api/v1/status/tsdb`) to fetch `seriesCountByMetricName`, `labelValueCountByLabelName`, `seriesCountByLabelPair`. Cross-reference with metrics and labels in dashboard queries to convert heuristic guesses ("this metric *might* be high-cardinality") into measured facts ("this metric has 5,247 active series"). Also query variable endpoints to fetch actual variable value counts for explosion detection. Cache TSDB status responses with 5-minute TTL — cardinality changes slowly. This layer upgrades `Confidence` on findings from ~0.5 to ~0.9.

**Layer 3 — Runtime telemetry correlation (Phase 3).** Enable `--query-frontend.log-queries-longer-than=5s` on Thanos and `query_log_file` on Prometheus. Build a log ingestion pipeline that parses JSON logs, extracts queries with durations and sample counts, then reverse-maps each expression to dashboard panels via the normalized expression index (see CLAUDE.md technical landmine about variable expansion mismatch). Output: per-panel `[query_duration_avg, series_touched, samples_processed, cache_hit_rate]`. This is the only layer that provides actual measured performance.

### Six custom capabilities to build (no OSS exists for these)

These represent the core value-add of this project. None exist in any open-source tool:

1. **Static PromQL cost estimator (`CostVisitor`)** — walks the parse tree, assigns weights based on selector count × estimated series, range window sizes, aggregation depth, regex complexity. See §8 for the formula. Fills the gap left by Thanos's unimplemented EXPLAIN proposal (issue #5911).

2. **Template-variable explosion detector** — calculates cross-product `Π(var_i.values_count)` for chained variables with `includeAll`, multiplied by repeat panel count. Flags dashboards where load would fire hundreds of concurrent queries. This is the #1 cause of "dashboard hangs when I select All."

3. **Panel-to-query reverse mapper (Phase 3)** — normalizes both templated expressions (from JSON) and expanded expressions (from Thanos logs) into a comparable form for correlation. Hardest problem in the project.

4. **Recording-rule suggestion engine (Phase 3)** — identifies expensive aggregation patterns appearing in multiple panels/dashboards, generates recording-rule YAML with appropriate naming (`level:metric:operations`) following sloth's pattern.

5. **Weighted composite scoring** — 0–100 dashboard health score. Tunable per-org thresholds and severity weights. Formula in CLAUDE.md.

6. **Dashboard JSON patch generator** — extends dashboard-linter's `--fix` with richer patching: replace hardcoded intervals with `$__rate_interval`, add missing label matchers, convert `=~"value"` to `="value"`, insert `minInterval`, adjust refresh/range. Outputs RFC 6902 JSON Patches for PR review or direct application.

---

## 2. System overview

```
┌─────────────────────────────────────────────────────────────────┐
│                  Presentation Layer                              │
│  CLI (JSON/text/SARIF) · Web UI (React) · Grafana App Plugin    │
└────────────────────────┬────────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────────┐
│               Analysis Engine (Go library: pkg/analyzer)         │
│                                                                  │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │  JSON Analyzer    │  │  PromQL Analyzer  │  │  Cardinality  │  │
│  │  (D-series rules) │  │  (Q-series rules) │  │  Enricher     │  │
│  │                   │  │                   │  │  (Phase 2)    │  │
│  └──────┬────────────┘  └──────┬────────────┘  └──────┬────────┘  │
│         └──────────────────────┼──────────────────────┘           │
│                                ▼                                  │
│                     Recommendation Engine                         │
│              scoring · fix generation · output                    │
└──────────────────────────────────────────────────────────────────┘
         │                    │                    │
   ┌─────▼─────┐     ┌───────▼──────┐    ┌───────▼──────────┐
   │ Grafana   │     │ Prometheus/  │    │ Thanos Query     │
   │ HTTP API  │     │ Thanos TSDB  │    │ Frontend Logs    │
   │           │     │ Status API   │    │ (Phase 3)        │
   └───────────┘     └──────────────┘    └──────────────────┘
```

The engine is a Go library (`pkg/analyzer`) that accepts dashboard JSON and returns a structured `Report`. Three presentation layers consume it: CLI, web UI, and Grafana plugin. They share the same library — the engine has no dependency on how results are displayed.

---

## 3. Core data types

```go
// Finding represents a single detected issue
type Finding struct {
    RuleID      string   // "Q1", "D2", "B1", etc. — stable, never renumbered
    Severity    Severity // Critical, High, Medium, Low
    PanelIDs    []int    // affected panel IDs (empty for dashboard-level findings)
    PanelTitles []string // human-readable panel names
    Title       string   // short: "Missing label filters"
    Why         string   // "This query selects all series for metric X without filtering..."
    Fix         string   // "Add label matchers: {job=\"$job\", namespace=\"$namespace\"}"
    Impact      string   // "Reduces series scanned by ~10-100×"
    Validate    string   // "Open Query Inspector → Stats tab → check series count before/after"
    AutoFixable bool     // true if --fix can patch this automatically
    Confidence  float64  // 0.0-1.0; lower for static-only analysis, higher with cardinality data
}

// Severity levels with scoring weights
type Severity int
const (
    Low      Severity = iota // weight: 2
    Medium                   // weight: 5
    High                     // weight: 10
    Critical                 // weight: 15
)

// Report is the output of analyzing one dashboard
type Report struct {
    DashboardUID   string
    DashboardTitle string
    Score          int        // 0-100 composite health score
    Findings       []Finding
    PanelScores    map[int]int // panel ID → per-panel score
    Metadata       ReportMeta
}

// Rule is the interface every detection rule implements
type Rule interface {
    ID() string
    Severity() Severity
    Check(ctx *AnalysisContext) []Finding
}

// AnalysisContext carries all data a rule might need
type AnalysisContext struct {
    Dashboard    *DashboardModel     // parsed JSON
    Panels       []PanelModel        // all panels with targets
    Variables    []VariableModel     // template variables
    ParsedExprs  map[string]parser.Expr // target expr string → parsed AST (cached)
    Cardinality  *CardinalityData    // nil in Phase 1 (static only)
}
```

---

## 4. Analysis pipeline

For each dashboard:

1. **Extract**: Fetch JSON via Grafana API or read from file. Deserialize into `DashboardModel`. Extract all panels (including nested row panels), targets, variables.

2. **Parse**: For every `target.Expr`, call `parser.ParseExpr()`. Cache results in `ParsedExprs` map (same expression may appear in multiple panels). Log and skip unparseable expressions.

3. **Analyze**: Run all registered rules against the `AnalysisContext`. Each rule returns zero or more `Finding` structs. Rules are independent and stateless — they can run in parallel.

4. **Score**: Compute composite score: `100 − Σ(severity_weight × len(findings_at_severity))`, clamped to [0,100]. Compute per-panel scores similarly.

5. **Output**: Format as JSON, human-readable text, or SARIF depending on CLI flags. For `--fix` mode, apply auto-fixable rules to produce a patched dashboard JSON.

---

## 5. Rule implementation guide

Each rule is a single file in `pkg/rules/`. Naming convention: `q1_missing_filters.go`, `d2_repeat_all.go`.

### Example: Q3 (regex where equality suffices)

```go
package rules

import (
    "github.com/prometheus/prometheus/promql/parser"
    "github.com/prometheus/prometheus/model/labels"
)

type RegexEquality struct{}

func (r *RegexEquality) ID() string       { return "Q3" }
func (r *RegexEquality) Severity() Severity { return Medium }

func (r *RegexEquality) Check(ctx *AnalysisContext) []Finding {
    var findings []Finding
    for _, panel := range ctx.Panels {
        for _, target := range panel.Targets {
            expr, ok := ctx.ParsedExprs[target.Expr]
            if !ok {
                continue
            }
            parser.Inspect(expr, func(node parser.Node, path []parser.Node) error {
                vs, ok := node.(*parser.VectorSelector)
                if !ok {
                    return nil
                }
                for _, m := range vs.LabelMatchers {
                    if m.Type == labels.MatchRegexp && !containsRegexMeta(m.Value) {
                        findings = append(findings, Finding{
                            RuleID:      "Q3",
                            Severity:    Medium,
                            PanelIDs:    []int{panel.ID},
                            PanelTitles: []string{panel.Title},
                            Title:       "Regex matcher where equality suffices",
                            Why:         fmt.Sprintf("Label %q uses regex match =~\"%s\" but the value contains no regex metacharacters. Regex matching is slower than equality.", m.Name, m.Value),
                            Fix:         fmt.Sprintf("Change %s=~\"%s\" to %s=\"%s\"", m.Name, m.Value, m.Name, m.Value),
                            Impact:      "Avoids regex engine overhead on every label lookup",
                            Validate:    "Query Inspector → Stats tab → compare query time before/after",
                            AutoFixable: true,
                            Confidence:  1.0, // purely structural, no ambiguity
                        })
                    }
                }
                return nil
            })
        }
    }
    return findings
}

// containsRegexMeta returns true if s contains regex metacharacters
func containsRegexMeta(s string) bool {
    for _, c := range s {
        switch c {
        case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '\\':
            return true
        }
    }
    return false
}
```

### Testing convention

Every rule has a `_test.go` file. Tests load panels from `slow-by-design.json` that trigger the rule and verify findings are produced. Tests also load from `fixed-by-advisor.json` and verify zero findings.

```go
func TestRegexEquality(t *testing.T) {
    slow := loadTestDashboard(t, "slow-by-design.json")
    ctx := buildContext(slow)
    rule := &RegexEquality{}
    findings := rule.Check(ctx)
    assert.NotEmpty(t, findings, "should detect regex-as-equality in slow dashboard")

    fixed := loadTestDashboard(t, "fixed-by-advisor.json")
    ctx = buildContext(fixed)
    findings = rule.Check(ctx)
    assert.Empty(t, findings, "should find no issues in fixed dashboard")
}
```

---

## 6. PromQL AST patterns to detect

Reference for implementing Q-series rules. All use `parser.Walk()` or `parser.Inspect()`.

### Finding VectorSelectors and their matchers (Q1, Q2, Q3, Q14)
```go
parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
    vs, ok := node.(*parser.VectorSelector)
    if !ok { return nil }
    // vs.LabelMatchers — slice of *labels.Matcher
    // Each has: .Type (MatchEqual, MatchNotEqual, MatchRegexp, MatchNotRegexp), .Name, .Value
    return nil
})
```

### Finding aggregation structure (Q4, Q5, Q10)
```go
parser.Inspect(expr, func(node parser.Node, path []parser.Node) error {
    agg, ok := node.(*parser.AggregateExpr)
    if !ok { return nil }
    // agg.Grouping — []string of label names in by()/without()
    // agg.Without — bool (true = without(), false = by())
    // agg.Expr — inner expression (check if it's another AggregateExpr for nesting)
    // For Q10: check if inner is a *parser.Call with Func.Name == "rate"/"increase"
    return nil
})
```

### Finding function calls and range vectors (Q6, Q7, Q8, Q11, Q13)
```go
parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
    call, ok := node.(*parser.Call)
    if !ok { return nil }
    // call.Func.Name — "rate", "irate", "increase", "label_replace", etc.
    // call.Args — []parser.Expr
    // For rate/irate/increase: first arg is usually a MatrixSelector
    if ms, ok := call.Args[0].(*parser.MatrixSelector); ok {
        // ms.Range — time.Duration (the [5m] part)
    }
    return nil
})
```

### Finding subqueries (Q8)
```go
parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
    sq, ok := node.(*parser.SubqueryExpr)
    if !ok { return nil }
    // sq.Range — outer range window
    // sq.Step — step interval (the [1h:10s] → Step = 10s)
    // sq.Expr — inner expression (check for nested subqueries)
    return nil
})
```

### Finding binary expressions / joins (Q12)
```go
parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
    bin, ok := node.(*parser.BinaryExpr)
    if !ok { return nil }
    if bin.VectorMatching != nil {
        // bin.VectorMatching.Card — CardOneToOne, CardManyToOne, CardManyToMany, CardOneToMany
        // bin.VectorMatching.MatchingLabels — []string
        // bin.VectorMatching.On — bool (true = on(), false = ignoring())
        // bin.VectorMatching.Include — []string (group_left/group_right labels)
    }
    return nil
})
```

---

## 7. Dashboard JSON fields reference

Key fields to extract from dashboard JSON for D-series rules:

```
dashboard.refresh          → D5 (check if < "30s")
dashboard.time.from/to     → D6 (check if range > 24h; parse relative like "now-7d")
dashboard.panels[]          → D1 (count; exclude collapsed row children)
  .id                      → panel identification
  .title                   → human-readable name
  .type                    → "row", "graph", "timeseries", "stat", etc.
  .repeat                  → D2 (non-null = repeated panel; value = variable name)
  .repeatDirection         → "h" or "v"
  .maxPerRow               → limits horizontal repeats
  .maxDataPoints           → D7 (check if absent or 0)
  .interval                → D7 (check if absent; should be ≥ scrape_interval)
  .targets[]               → query expressions
    .expr                  → the PromQL string
    .legendFormat           → (informational)
    .datasource            → D9 (check for mixing)
  .collapsed               → D10 (if type=="row" and collapsed==false, panels inside load immediately)
dashboard.templating.list[] → D3, D4
  .name                    → variable name (referenced as $name in queries)
  .type                    → "query", "custom", "constant", "datasource", "interval", "textbox"
  .query                   → D4 (for type=="query": check if it's label_values() vs full PromQL)
  .includeAll              → D2/D3 (bool)
  .multi                   → D3 (bool — allows multi-select)
  .refresh                 → 0 (never), 1 (on dashboard load), 2 (on time range change)
  .allValue                → custom value for "All" option
  .regex                   → filters variable values
```

---

## 8. Demo dashboard: panel-to-rule mapping

The `slow-by-design.json` dashboard must contain these panels, each triggering specific rules. This is the test fixture for the entire project.

| Panel title | PromQL expression | Anti-patterns | Rules triggered |
|---|---|---|---|
| "Global Request Rate" | `sum(rate(http_requests_total[5m]))` | Bare metric, no label filters, hardcoded range | Q1, Q7 |
| "Error Ratio" | `sum(rate(http_requests_total{status=~".*error.*"}[5m])) / sum(rate(http_requests_total[5m]))` | Unbounded regex, missing filters | Q1, Q2 |
| "Status Codes" | `sum by(status) (rate(http_requests_total{status=~"200"}[5m]))` | Regex where equality works | Q3 |
| "Latency by Pod" | `histogram_quantile(0.99, sum by(pod, container, instance, namespace, le) (rate(http_request_duration_seconds_bucket[5m])))` | High-cardinality grouping (5 dims) | Q4 |
| "Total Throughput" | `rate(sum(http_requests_total)[5m])` | rate wrapping sum (wrong order) | Q10 |
| "P99 over 1h" | `histogram_quantile(0.99, sum by(le) (rate(http_request_duration_seconds_bucket[1h])))` | Huge rate range (1h) | Q6 |
| "Smoothed CPU" | `avg_over_time(rate(node_cpu_seconds_total{mode="idle"}[5m])[1h:10s])` | Fine-resolution subquery | Q8 |
| "Memory Usage 1" | `process_resident_memory_bytes{job="prometheus"}` | Duplicated in 4 panels | Q9 |
| "Memory Usage 2" | `process_resident_memory_bytes{job="prometheus"}` | (duplicate) | Q9 |
| "Memory Usage 3" | `process_resident_memory_bytes{job="prometheus"}` | (duplicate) | Q9 |
| "Memory Usage 4" | `process_resident_memory_bytes{job="prometheus"}` | (duplicate) | Q9 |
| "Late Agg Example" | `sum(node_filesystem_avail_bytes)` | Aggregation over unfiltered metric | Q5 |
| "Goroutine Count" | `rate(go_goroutines[5m])` | rate on gauge (should be deriv or avg_over_time) | Q11 |
| "Request Rate by Instance" | repeated panel | `repeat: instance`, variable has includeAll: true | D2 |
| 15+ other panels | various | Padding to exceed 25 visible panels | D1 |

**Dashboard-level settings for the slow version:**
- `refresh: "10s"` → triggers D5
- `time.from: "now-7d"` → triggers D6
- No `maxDataPoints` on any panel → triggers D7
- No collapsed rows → triggers D10
- Variable `$instance`: query is `count by(instance) (up)` (full PromQL) → triggers D4
- Variable `$pod`: has `includeAll: true`, `multi: true`, backed by high-cardinality label → triggers D3
- Multiple datasource UIDs across panels → triggers D9

**The fixed version** corrects every issue: adds filters, simplifies regex, reorders aggregation, reduces range, sets maxDataPoints, uses collapsed rows, sets refresh to "1m", range to "now-1h", variable queries use `label_values()`, repeat variable has regex filter limiting to 10 values.

---

## 9. CostVisitor design (Phase 2)

Walks the PromQL AST and produces a numeric cost estimate per query. Used for ranking panels by expense.

```
cost = Σ(selector_costs) × aggregation_factor × function_factor

selector_cost = estimated_series(metric_name) × (range_seconds / step_seconds)
aggregation_factor = 1.0 + (0.2 × nesting_depth) + (0.1 × len(grouping_labels))
function_factor = base_cost(func_name)  // rate=1.0, histogram_quantile=2.0, sort=0.5, etc.
```

`estimated_series` comes from TSDB status API in Phase 2. In Phase 1, use heuristic defaults: unknown metric = 1000 series (medium assumption).

---

## 10. Pint checks to port (Phase 2)

These files from `cloudflare/pint` `internal/checks/` should be forked and adapted:

| Pint check | Our rule | Lines (approx) | Dependencies |
|---|---|---|---|
| `promql/regexp.go` | Q2, Q3 | ~300 | Prometheus parser only |
| `promql/selector.go` | Q1, Q14 | ~400 | Prometheus parser + optional live query |
| `promql/rate.go` | Q11 | ~250 | Prometheus parser + metric type metadata |
| `promql/vector_matching.go` | Q12 | ~200 | Prometheus parser only |
| `promql/impossible.go` | (future) | ~150 | Prometheus parser + live query |
| `query/cost.go` | CostVisitor | ~350 | Prometheus parser + live query |

Total: ~1,650 lines of well-structured Go to review and adapt. Each is self-contained and depends only on the Prometheus parser library.

---

## 11. Demo-driven roadmap

The MVP starts with a demo, not ends with one. Week 1 produces a running stack. Every subsequent rule is validated against the `slow-by-design.json` dashboard. Stakeholders can try the product at the end of any week.

### Phase 1: Demo + static analysis + CLI + web UI (weeks 1–6)

**Week 1: Demo environment.**
- `docker-compose.yml` with: Prometheus (scraping itself + node-exporter + synthetic exporter), Thanos sidecar + querier (no query-frontend — intentionally), Grafana provisioned with both dashboards.
- Synthetic exporter: small Go binary using `prometheus/client_golang` that generates `http_requests_total` and `http_request_duration_seconds_bucket` with ~5,000 series across labels (`method`, `status`, `path`, `pod`, `container`, `instance`, `namespace`).
- Provision `slow-by-design.json` and `fixed-by-advisor.json` dashboards via Grafana provisioning.
- README: `docker-compose up` → open Grafana → load slow dashboard → experience visible slowness.
- **Checkpoint**: The demo stack runs. The slow dashboard is noticeably slow. The fixed dashboard is noticeably fast.

**Week 2–3: Analysis engine core (first 8 rules).**
- Implement `pkg/analyzer/engine.go`, `pkg/rules/rule.go` (Finding struct, Rule interface).
- Implement 8 rules: Q1, Q2, Q3, Q10, D1, D2, D5, D7.
- Each rule has a `_test.go` that loads both demo dashboards and asserts: findings > 0 for slow, findings == 0 for fixed.
- Composite scoring works.
- **Checkpoint**: `go test ./pkg/...` passes. Engine produces 8+ findings for slow dashboard, 0 for fixed.

**Week 4: CLI + remaining static rules.**
- `cmd/dashboard-advisor/main.go` — reads JSON from file or Grafana API.
- Output formats: `--format=json|text|sarif`.
- `--fail-on=high|medium|low` for CI gates.
- `--fix` mode for auto-fixable rules (Q3, Q7, D5, D6, D7).
- Add remaining rules: Q4, Q5, Q6, Q7, Q8, Q9, D4, D6, D8, D9, D10.
- **Checkpoint**: `dashboard-advisor lint demo/dashboards/slow-by-design.json` prints 15+ findings with score. `dashboard-advisor fix demo/dashboards/slow-by-design.json --output /tmp/patched.json` produces a dashboard comparable to `fixed-by-advisor.json`.

**Week 5–6: Web UI.**
- React web app (standalone Docker container, added to docker-compose).
- Dashboard list view: health scores, panel count, finding count.
- Dashboard detail view: recommendation cards with severity badge, affected panels, "Why this is slow", "What to change" (with PromQL diff), "Expected impact", "How to validate".
- Filter by severity. Mute/resolve per recommendation.
- **Checkpoint**: Full end-to-end flow — slow dashboard in Grafana (tab 1), advisor web UI (tab 2), click through recommendations, panel IDs link back to Grafana.

### Phase 2: Live enrichment + Grafana plugin (weeks 7–10)

**Week 7–8: Cardinality enrichment + CostVisitor.**
- TSDB status API client (`/api/v1/status/tsdb`).
- `CostVisitor` implementation (see §9).
- Template-variable explosion detection with live variable value counts.
- Thanos checks: B1 (query-frontend detection), B2 (cache hit rates), B3 (slow query logging).
- Port pint checks: fork `promql/regexp.go`, `promql/selector.go`, `promql/rate.go` from pint `internal/`.
- **Demo evolution**: Add Thanos query-frontend as optional docker-compose profile (`docker-compose --profile optimized up`). Advisor detects its absence in default mode, confirms presence in optimized mode. Findings now show actual series counts instead of estimates.

**Week 9–10: Grafana App Plugin.**
- Scaffold with `@grafana/create-plugin`.
- Implement `checks.Check` interface from `grafana-advisor-app`.
- Backend Go component running the analysis engine.
- "Analyze this dashboard" link in dashboard settings.
- Service account auth.
- Test across Grafana 10.x, 11.x, 12.x.
- **Demo evolution**: Advisor is now inside Grafana — no second tab needed.

### Phase 3: Runtime profiling + advanced features (weeks 11–16)

**Week 11–12: Query replay + telemetry correlation.**
- Replay dashboard queries via `/api/ds/query`, measure timing/series/samples.
- Panel-to-query reverse mapper with normalization layer.
- Ingest Thanos slow query logs + Prometheus `query_log_file`.
- Calibrate scoring model: compare estimated vs. measured cost.
- **Demo evolution**: Advisor shows measured query times per panel ("Panel 7: avg 4.2s, 120k series") alongside estimated cost.

**Week 13–14: Recording-rule generation.**
- Identify candidates: duplicated expensive aggregation patterns.
- Generate YAML following `level:metric:operations` naming.
- Export as PrometheusRule CRD YAML.
- Estimate cost savings per rule.
- **Demo evolution**: Advisor generates `recording-rules.yaml`. Apply to Prometheus, re-run advisor — score improves.

**Week 15–16: Trend tracking + alerting.**
- Store analysis results over time (SQLite or PostgreSQL).
- Score trend visualization in web UI.
- Alert on score drops (new panels added, cardinality explosion).
- Grafana alerting integration.
- **Demo evolution**: Script that progressively adds bad panels to the dashboard over simulated time, showing score degradation on a trend chart.

---

## 12. Detection heuristics — per-rule specifications

Concise implementation spec for each rule. For PromQL AST patterns, see §6.

### Q-series (PromQL)

**Q1 — Missing label filters.** Walk AST for `*VectorSelector` nodes. Count `LabelMatchers` excluding `__name__`. If count ≤ 0 (bare metric), severity = Critical. If count == 1 and it's only `job`, severity = High. The fix should suggest adding `namespace`, `cluster`, or other scoping labels contextually.

**Q2 — Unbounded regex.** Check each `LabelMatcher` with `Type == MatchRegexp`. Flag if value starts with `.*`, contains `.*` in the middle without anchored prefix, or is `.+`. Exclude `__name__` matchers (those are common). The fix should suggest removing regex or anchoring it.

**Q3 — Regex as equality.** Check each `LabelMatcher` with `Type == MatchRegexp`. Call `containsRegexMeta()` on value — if false, it's a plain string used as regex. Auto-fix: change type to `MatchEqual` in the expression string (string replacement of `=~"value"` → `="value"`).

**Q4 — High-cardinality grouping.** Find `*AggregateExpr` nodes. Check `Grouping` slice length — flag if >3 labels. Also flag if any label name is in a known high-cardinality set: `pod`, `container`, `instance`, `pod_name`, `container_name`, `id`, `uid`. Phase 2 enriches this with actual label cardinality from TSDB status.

**Q5 — Late aggregation.** Find `*AggregateExpr` where the inner expression contains `*VectorSelector` nodes with ≤1 matcher. The aggregation should happen *after* filtering, not wrap an unfiltered selector.

**Q6 — Long rate ranges.** Find `*Call` with `Func.Name` in `["rate", "irate", "increase", "delta", "idelta"]`. First arg should be `*MatrixSelector` — check `Range`. Flag if >10m. The fix should suggest `$__rate_interval` (see Q7) or recording rules for long-window cases.

**Q7 — Hardcoded interval.** Find `*MatrixSelector` inside rate/irate/increase calls. Check if the `Range` value is a literal duration (not derived from a Grafana variable). In the raw expression string, check if the range bracket content matches a literal like `[5m]` vs `[$__rate_interval]` or `[$__interval]`. Auto-fix: replace the literal with `[$__rate_interval]`.

**Q8 — Subquery abuse.** Find `*SubqueryExpr` nodes. Flag if: (a) nested (SubqueryExpr contains another SubqueryExpr), (b) step < 1m with range > 1h, or (c) range/step ratio > 360 (would generate >360 inner evaluations).

**Q9 — Duplicate expressions.** After parsing all targets across all panels, normalize expression strings (strip whitespace, sort label matchers alphabetically within each selector). Hash the normalized strings. Group by hash. Flag groups with >2 panels using the same expression.

**Q10 — Incorrect aggregation order.** Find `*Call` with `Func.Name` in `["rate", "irate", "increase"]` where the argument is `*AggregateExpr` (or a `*StepInvariantExpr` wrapping one). This detects `rate(sum(x)[5m])` which is mathematically wrong and should be `sum(rate(x[5m]))`.

### D-series (Dashboard JSON)

**D1 — Too many panels.** Count `dashboard.panels[]` where `type != "row"`. Exclude panels inside collapsed rows (these don't fire queries on load). Flag if visible count > 25. Threshold should be configurable.

**D2 — Repeat with All.** For each panel with `repeat` set (non-null), find the variable it references in `templating.list[]`. Flag if that variable has `includeAll: true`. Severity scales with estimated variable cardinality if available.

**D3 — Variable explosion.** For all variables with `includeAll: true` and `multi: true`, estimate value count (Phase 1: use a default of 100 if unknown; Phase 2: query the datasource). Calculate cross-product of all such variables that are referenced in the same panels or repeat directives. Flag if product > 50.

**D4 — Expensive variable query.** For each variable with `type == "query"`, check if the `.query` string starts with `label_values(` (cheap — reads from index). If not, it's a full PromQL query that must be evaluated. Flag full PromQL variable queries as High.

**D5 — Refresh too frequent.** Parse `dashboard.refresh` (string like `"10s"`, `"1m"`, `"5m"`, or `""` for off). Flag if parsed duration < 30s. Auto-fix: set to `"1m"`.

**D6 — Range too wide.** Parse `dashboard.time.from` (string like `"now-7d"`, `"now-24h"`). Extract the duration. Flag if > 24h. Auto-fix: set to `"now-1h"`.

**D7 — Missing maxDataPoints.** For each panel with `type` in `["timeseries", "graph", "barchart", "heatmap"]`, check if `maxDataPoints` is absent, null, or 0. Auto-fix: set to `1000`.

**D8 — Duplicate queries.** Same as Q9 but reported as a dashboard-level finding with the fix being "use Dashboard data source to share query results."

**D9 — Datasource mixing.** Collect all distinct `datasource.uid` values across panels. Flag if > 2 distinct UIDs (excluding template variable datasources like `$datasource`).

**D10 — No collapsed rows.** Check if any panel with `type == "row"` has `collapsed: true`. If no row panels exist, or all rows have `collapsed: false`, flag as Medium.

---

## 13. Failure modes

| Scenario | Behavior |
|----------|----------|
| Grafana API unreachable | CLI exits with code 2 and error message. Web UI shows connection error. File-based analysis unaffected. |
| Grafana API returns 401/403 | Log auth error with URL. Suggest checking API key and Viewer role. |
| Dashboard JSON malformed | Log dashboard UID and skip. Continue analyzing remaining dashboards. Never crash on one bad dashboard. |
| PromQL expression unparseable | Log the expression string and panel ID. Skip the expression. Return findings for parseable expressions. `Confidence` on remaining findings unaffected. |
| TSDB status API unreachable (Phase 2) | Fall back to Phase 1 heuristic defaults (estimated 1000 series per unknown metric). Set `Confidence` to 0.5 on cardinality-dependent findings. |
| TSDB status API returns unexpected format | Log response and skip cardinality enrichment. Degrade gracefully to static analysis. |
| Thanos query-frontend logs unavailable (Phase 3) | Telemetry correlation produces no results. Static analysis and cardinality enrichment still work. |
| Dashboard has zero panels | Return a Report with score 100, empty Findings, and a metadata note. Not an error. |
| Panel has no targets | Skip the panel for Q-series rules. D-series rules (panel count, collapsed rows) still apply. |
| Variable query returns error | Log the variable name and error. Use default cardinality estimate for explosion detection. |
| `--fix` produces invalid JSON | Validate patched JSON against Grafana schema before writing. If validation fails, write original + findings report, log the fix failure. |
| Scoring produces negative number | Clamp to 0. This means the dashboard has many critical issues — the score floor is meaningful. |

**Design principle**: The advisor is a read-only analysis tool. It never modifies dashboards in Grafana, never writes to Prometheus/Thanos, and never blocks any user workflow. Failures in one component (cardinality lookup, single rule, single panel) must never prevent the rest of the analysis from completing.

---

## 14. Known limitations

- **Static analysis has false positives.** Without runtime cardinality data (Phase 1), a metric with 10 series and a metric with 100,000 series look the same. Phase 2 cardinality enrichment reduces this significantly.
- **Grafana variable values not available offline.** When analyzing dashboard JSON from files (not via API), template variable cardinality cannot be resolved. Explosion detection (D3) uses conservative defaults.
- **No SQL/InfluxQL/LogQL parsing.** Only PromQL expressions are analyzed. Dashboards using other query languages pass through unanalyzed with a note.
- **No Grafana Transformations analysis.** Client-side transformations (merge, filter, join) applied in the browser are not visible in dashboard JSON targets.
- **No cross-dashboard deduplication in file mode.** When analyzing individual files, Q9 (duplicate expressions) only detects duplicates within a single dashboard, not across dashboards. API mode with batch analysis can detect cross-dashboard duplicates.
- **Repeat panel expansion is estimated, not measured.** The advisor calculates `repeat_count = len(variable_values)` but cannot know the actual runtime expansion without loading the dashboard in Grafana.
- **Recording-rule suggestions (Phase 3) require manual deployment.** The advisor generates YAML but cannot apply it to Prometheus/Thanos. The user must deploy recording rules through their own pipeline.

---

## 15. External references

- Grafana dashboard-linter: github.com/grafana/dashboard-linter
- Grafana advisor-app: github.com/grafana/grafana-advisor-app
- Cloudflare pint: github.com/cloudflare/pint
- Prometheus PromQL parser: github.com/prometheus/prometheus/promql/parser
- Grafana tools SDK: github.com/grafana-tools/sdk
- Grafana foundation SDK: github.com/grafana/grafana-foundation-sdk
- Sloth (recording-rule patterns): github.com/slok/sloth
- Thanos query-frontend docs: thanos.io/tip/components/query-frontend.md/
- Thanos EXPLAIN proposal: github.com/thanos-io/thanos/issues/5911
- Grafana query optimization blog: grafana.com/blog/grafana-dashboards-tips-for-optimizing-query-performance/
- Prometheus TSDB status API: prometheus.io/docs/prometheus/latest/querying/api/ (TSDB status section)

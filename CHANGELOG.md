# Dashboard Performance Advisor — Changelog

> Historical record of completed phases, bug post-mortems, and lessons learned.
> For active coding standards see [CLAUDE.md](CLAUDE.md). For architecture see [ARCHITECTURE.md](ARCHITECTURE.md).
> For the research behind project decisions see [docs/RESEARCH.md](docs/RESEARCH.md).

---

## Phase Specs and Status

### Phase 1: Demo + Static Analysis + CLI + Web UI (weeks 1–6)

| Week | Deliverable | Status | Tests |
|------|------------|--------|-------|
| 1 | Docker-compose demo stack + `slow-by-design.json` + `fixed-by-advisor.json` + synthetic exporter | [x] | 5 containers up, dashboards provisioned, metrics flowing |
| 2–3 | Analysis engine core — first 8 rules (Q1, Q2, Q3, Q10, D1, D2, D5, D7) + scoring | [x] | Unit: 92 findings on slow, 0 on fixed |
| 4 | CLI + remaining static rules (Q4–Q9, D4, D6, D8–D10) + `--fix` mode | [x] | Unit: 92 findings on slow, 0 on fixed. Integration: `--fix` reduces to 41 findings |
| 5–6 | Web UI with recommendation cards | [x] | Manual: upload JSON → score gauge + finding cards + auto-fix download |

### Phase 2: Live Enrichment + Grafana Plugin (weeks 7–10)

| Week | Deliverable | Status | Tests |
|------|------------|--------|-------|
| 7–8 | TSDB status API integration + CostVisitor + variable explosion detection + Thanos checks (B1–B3) + pint check ports | [ ] | Unit: cardinality-enriched findings have Confidence > 0.8 |
| 9–10 | Grafana App Plugin (`checks.Check` interface) | [ ] | Manual: advisor embedded in Grafana 10/11/12 |

### Phase 3: Runtime Profiling + Advanced Features (weeks 11–16)

| Week | Deliverable | Status | Tests |
|------|------------|--------|-------|
| 11–12 | Query replay + telemetry correlation (panel-to-query reverse mapper) | [ ] | Integration: measured query times appear per panel |
| 13–14 | Recording-rule generation (sloth pattern) | [ ] | Unit: generated YAML is valid PrometheusRule CRD |
| 15–16 | Trend tracking + alerting on score degradation | [ ] | Unit: score history stored and queryable |

---

## Completed Work

### Web UI readability improvements (2026-02-15)

**Problem:** Users saw rule IDs like "Q1", "D2" in the web UI but had no context for what they meant or which panels were affected. All detail was hidden behind expanding each card.

**Changes (all in `web/index.html`):**
- Colored left borders on finding cards by severity (red=critical, yellow=high, blue=medium, gray=low) for at-a-glance scanning
- Panel names shown in collapsed header (up to 2, with "+N more" overflow) — no click needed to see which panels are affected
- "Why" one-liner preview visible in collapsed view, truncated with ellipsis
- Rule ID styled as a badge with tooltip ("PromQL query rule" / "Dashboard design rule")
- Finding title weight increased to 600 for better visual hierarchy
- Header layout changed from single-line to multi-line (title → panels → why)

**Files changed:** `web/index.html` (CSS + `renderFindings()` JS function)

---

### Phase 1, Weeks 1 + 5–6: Docker demo stack + Web UI (2026-02-15)

**Docker Compose demo stack (Week 1 — previously deferred):**
- `cmd/demo-exporter/main.go` — synthetic exporter generating all metrics referenced by demo dashboards (http_requests_total, node_cpu_seconds_total, histograms, etc.) with configurable cardinality (2 instances, 10 pods, 3 namespaces)
- `Dockerfile.exporter` — multi-stage Go build
- `demo/prometheus/prometheus.yml` — 5s scrape interval, `honor_labels: true`, external_labels for Thanos
- `demo/grafana/provisioning/` — auto-provisions 3 datasources (prometheus-main, prometheus-secondary, thanos-querier) and both demo dashboards
- `docker-compose.yml` — 5 services: exporter, prometheus, thanos-sidecar, thanos-querier, grafana (anonymous admin)

**Web UI (Weeks 5–6):**
- `web/index.html` — self-contained SPA (dark theme, no dependencies, no npm). Score gauge, metadata bar, finding cards grouped by severity, auto-fix download button
- `web/embed.go` — `go:embed` for single-binary deployment
- `pkg/server/server.go` — HTTP handlers: `POST /api/analyze`, `POST /api/fix`, `GET /`
- `pkg/analyzer/engine.go` — added `AnalyzeBytes([]byte)` method for HTTP API
- `cmd/dashboard-advisor/main.go` — added `--serve` and `--addr` flags

**Demo dashboard fixes (discovered during integration):**
- Fixed `instance` variable: changed from structured `qryType` object to plain string `query_result(count by(instance) (up))` with regex to extract clean values
- Added `description` to Total Throughput panel explaining Q10 is broken by design
- Added `"error"` status to exporter so Error Ratio panel shows data
- Reduced instances from 20→2 to prevent browser overload from repeat panels

**Verification:** All tests pass. slow-by-design: 92 findings, score 12/100. fixed-by-advisor: 0 findings, score 100/100.

---

### Scoring formula change: linear → asymptotic (2026-02-14)

**Problem:** The old scoring formula (`100 − Σ(severity_weight)`, clamped to 0) hid progress on severely bad dashboards. The slow-by-design dashboard scored 0/100, and after `--fix` removed 50 findings it still scored 0/100 — no visible improvement. Users couldn't see that auto-fix helped.

**Fix:** Replaced with asymptotic formula: `round(100 × k / (penalty + k))` where `k = 100`. Properties:
- Score approaches 0 but never reaches it — every fix always moves the needle
- No clamping needed; formula naturally stays in (0, 100]
- penalty = k (100 points) → score = 50 (midpoint)

**New scores:**
| Dashboard | Old Score | New Score |
|---|---|---|
| slow-by-design.json (92 findings) | 0/100 | 12/100 |
| After --fix (41 findings) | 0/100 | 17/100 |
| fixed-by-advisor.json (0 findings) | 100/100 | 100/100 |

**Files changed:** `pkg/rules/rule.go` (ComputeScore), `pkg/rules/rule_test.go`, CLAUDE.md, ARCHITECTURE.md, docs/RESEARCH.md, CHANGELOG.md

---

### Phase 1, Weeks 1–4: Demo dashboards + full analysis engine + CLI + auto-fix

**Demo dashboards:**
- `demo/dashboards/slow-by-design.json` — 32 panels (30 visible), triggers all 20 rules, 3 datasource UIDs, template variables with includeAll/multi
- `demo/dashboards/fixed-by-advisor.json` — 18 panels (10 visible), triggers 0 rules, proper label filters, $__rate_interval, collapsed rows

**Core types & extractor:**
- `pkg/rules/rule.go` — Finding, Severity, Rule interface, AnalysisContext, ComputeScore
- `pkg/extractor/models.go` — DashboardModel, PanelModel, VariableModel, TargetModel
- `pkg/extractor/extractor.go` — LoadDashboard, AllPanels, VisiblePanels, PanelsWithTargets, AllTargetExprs, AllDatasourceUIDs

**PromQL parser integration:**
- `pkg/analyzer/parser.go` — ParseAllExprs with Grafana template variable replacement ($__rate_interval → 5m, $var → placeholder)

**20 detection rules (all with tests):**
- Q-series (PromQL): Q1 missing filters, Q2 unbounded regex, Q3 regex-as-equality, Q4 high-cardinality grouping, Q5 late aggregation, Q6 long rate range, Q7 hardcoded interval, Q8 subquery abuse, Q9 duplicate expressions, Q10 incorrect aggregation order
- D-series (dashboard): D1 too many panels, D2 repeat-with-all, D3 variable explosion, D4 expensive variable query, D5 refresh too frequent, D6 range too wide, D7 missing maxDataPoints, D8 duplicate queries, D9 datasource mixing, D10 no collapsed rows

**Engine & CLI:**
- `pkg/analyzer/engine.go` — full pipeline: extract → parse → analyze → score
- `cmd/dashboard-advisor/main.go` — `--format=text|json`, `--fail-on=severity`, `--fix --output=path`
- `pkg/output/text.go`, `json.go` — human-readable and JSON formatters

**Auto-fixer:**
- `pkg/fixer/fixer.go` — fixes for Q3 (regex→equality), Q7 (hardcoded→$__rate_interval), D5 (refresh→1m), D6 (range→now-1h), D7 (add maxDataPoints:1000)
- Reduces slow dashboard from 92 findings to 41, eliminating all 50 auto-fixable findings

**Tests:** 45+ tests across 4 packages — all pass. Every rule tested against both demo dashboards (slow=findings, fixed=clean).

**Files changed:** 40+ files across cmd/, pkg/analyzer/, pkg/extractor/, pkg/fixer/, pkg/output/, pkg/rules/, demo/dashboards/

---

## Bug Post-Mortems

*(Record every bug using this format. Add a regression test before fixing.)*

### Template:

```
### Bug N: <short title>
- **Symptom**: What exactly was wrong? What did the user see?
- **Root cause**: Why did it happen? Which assumption was wrong?
- **Fix**: What code changed?
- **Why missed**: Why didn't existing tests catch it?
- **Regression test**: Which test file/function prevents recurrence?
```

---

## Lessons Learned

*(Operational gotchas and hard-won knowledge. Add entries as they're discovered.)*

1. **Grafana template variables break the PromQL parser.** `$__rate_interval`, `$variable`, `${variable}` are not valid PromQL. We pre-process expressions to replace duration vars with `5m` and label vars with `placeholder` before parsing. This means AST analysis sees normalized values, not the actual variable names.
2. **`rate(sum(metric)[5m])` is syntactically invalid PromQL.** The Prometheus parser rejects it ("ranges only allowed for vector selectors"). Q10 detects this via string-level pattern matching, not AST walking.
3. **Go regex replacement treats `$` as special.** When replacing hardcoded intervals with `$__rate_interval` in the fixer, must use `$$` in the replacement string to produce a literal `$`.
4. **D8/Q9 duplicate thresholds must be >2 (not >=2).** Two panels sharing a query is normal (e.g., timeseries + stat showing same metric). Only flag at 3+ panels to avoid false positives on the fixed dashboard.
5. **Linear scoring with clamping hides progress.** `100 − penalty` clamped to 0 means a dashboard with 92 findings and one with 41 findings both score 0 — demoralizing and uninformative. Asymptotic formula (`100 × k / (penalty + k)`) ensures every fix is visible in the score. No industry tool (Lighthouse, SonarQube, CodeClimate) uses linear-clamp scoring for this reason.
6. **Grafana variable `qryType` is unreliable across versions.** The structured variable query object (`qryType: 0–5`) maps to different backend behaviors across Grafana 10/11/12 and often sends queries to the wrong endpoint (e.g., `/api/v1/series` instead of `/api/v1/query`). Use a plain string `query` field with `query_result(expr)` wrapper for raw PromQL — this uses Grafana's classic query mode which is stable.
7. **`query_result()` returns formatted strings, not raw label values.** Output looks like `{instance="foo:9090"} 1`. A regex on the variable definition is required to extract the actual value.
8. **`honor_labels: true` is mandatory when exporters set their own `job`/`instance` labels.** Without it, Prometheus silently overwrites them with the scrape config's job name and target address. This caused all queries with `job="api-server"` to return no data because everything was `job="exporter"`.
9. **Thanos sidecar exits immediately without `external_labels`.** Prometheus must have at least one `external_labels` entry for Thanos sidecar to start. Not mentioned in most quick-start guides.
10. **Prometheus auto-generates `up` metrics per scrape target.** Even with `honor_labels: true`, Prometheus creates `up{instance="exporter:9099"}` alongside exporter-emitted `up` metrics. Variable queries on `up` will return this extra instance — filter with regex.
11. **Demo dashboards with `repeat` panels can crash the browser.** 20 instances × 1 repeat panel = 20 panels firing queries simultaneously. Keep cardinality low (2–5 instances) for demo environments. The D2 rule exists to catch this in production.
12. **"Bad by design" panels must still be functional.** A demo dashboard that shows "No data" everywhere teaches nothing. Every anti-pattern panel must render data — except panels demonstrating syntactically invalid PromQL (mark those clearly in the title).

---

## Systemic Test Gaps

Track known gaps in test coverage. When a bug reveals a gap, add it here so it's visible.

| Gap | Risk | Mitigation |
|-----|------|------------|
| No real Grafana API integration tests | Schema assumptions may be wrong for Grafana 10/11/12 | Test against real Grafana JSON exports from each version |
| No real Prometheus parser edge cases | Thanos PromQL extensions may cause parse errors | Log and skip unparseable expressions; collect examples |
| No multi-dashboard batch analysis tests | Cross-dashboard deduplication (Q9) untested at scale | Add test with 3+ dashboards sharing queries |
| No malformed JSON resilience tests | Real-world dashboards may have unexpected structures | Add fuzz-style tests with partial/broken JSON |
| Demo dashboards are synthetic | Real production dashboards may have patterns we don't anticipate | Collect anonymized dashboard JSON from real environments for testing |

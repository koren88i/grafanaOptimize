# Dashboard Performance Advisor — Changelog

> Historical record of completed phases, bug post-mortems, and lessons learned.
> For active coding standards see [CLAUDE.md](CLAUDE.md). For architecture see [ARCHITECTURE.md](ARCHITECTURE.md).
> For the research behind project decisions see [docs/RESEARCH.md](docs/RESEARCH.md).

---

## Phase Specs and Status

### Phase 1: Demo + Static Analysis + CLI + Web UI (weeks 1–6)

| Week | Deliverable | Status | Tests |
|------|------------|--------|-------|
| 1 | Docker-compose demo stack + `slow-by-design.json` + `fixed-by-advisor.json` + synthetic exporter | [partial] | Demo dashboards created; Docker stack deferred |
| 2–3 | Analysis engine core — first 8 rules (Q1, Q2, Q3, Q10, D1, D2, D5, D7) + scoring | [x] | Unit: 92 findings on slow, 0 on fixed |
| 4 | CLI + remaining static rules (Q4–Q9, D4, D6, D8–D10) + `--fix` mode | [x] | Unit: 92 findings on slow, 0 on fixed. Integration: `--fix` reduces to 41 findings |
| 5–6 | React web UI with recommendation cards | [ ] | Manual: end-to-end flow across Grafana + advisor UI |

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

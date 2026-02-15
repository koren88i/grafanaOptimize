# Dashboard Performance Advisor

> Auto-loaded by Claude Code. Keep this lean — only actionable rules and references.
> For architecture, data types, and pipeline see [ARCHITECTURE.md](ARCHITECTURE.md).
> For project history and bug post-mortems see [CHANGELOG.md](CHANGELOG.md).
> For the full research behind decisions (competitor analysis, OSS landscape, detection heuristic rationale) see [docs/RESEARCH.md](docs/RESEARCH.md).

## What this project is

A tool that analyzes Grafana dashboards and outputs actionable recommendations to make them faster. It answers "why is this dashboard slow?" and "what specifically should I change?" with estimated impact.

Our stack: Grafana → PromQL → Thanos → Prometheus. The advisor inspects dashboard JSON, parses every PromQL query into an AST, detects anti-patterns, scores the dashboard 0–100, and generates fixes.

**Goal check**: Every task should serve the core mission — helping users find and fix slow Grafana dashboards. Push back on features, abstractions, or complexity that don't directly support detection, scoring, or fix generation. If it doesn't make a dashboard faster or help someone understand why it's slow, it probably doesn't belong here.

## Audience

This project will be maintained by a small platform engineering team. Optimize for readability and maintainability:

- Prefer explicit over clever. Obvious code > elegant code.
- Prefer flat over nested. Avoid deep abstraction layers that require jumping between files to understand a flow.
- Name things for clarity, not brevity. `findMissingLabelFilters` > `checkQ1`.
- Keep dependencies minimal. Every added library is something the team needs to learn and maintain.
- Comments should explain *why*, not *what*. If the *what* isn't obvious, simplify the code.

## Who uses this and how

**Platform engineers** run it against all dashboards in an org to get a ranked list by health score, then work through the worst offenders. **Dashboard authors** run it on a single dashboard to get specific per-panel recommendations with "why" and "how to fix." **CI pipelines** run it as a lint gate on dashboard JSON PRs (`--fail-on=high`) to prevent new anti-patterns from shipping.

## Tech stack (decided, do not change)

- **Language**: Go
- **Dashboard JSON parsing**: `github.com/grafana/dashboard-linter/lint` (Apache-2.0) — import as library, extend with custom rules via `NewTargetRuleFunc`, `NewPanelRuleFunc`, `NewDashboardRuleFunc`
- **PromQL parsing**: `github.com/prometheus/prometheus/promql/parser` (Apache-2.0) — `parser.ParseExpr()` → `parser.Walk(visitor, node)` for AST traversal
- **Grafana API client**: `github.com/grafana-tools/sdk` (Apache-2.0) — for dashboard enumeration and variable resolution
- **Grafana App Plugin (Phase 2)**: Build on `github.com/grafana/grafana-advisor-app` (Apache-2.0) — implement its `checks.Check` interface with `Items()` returning dashboards and `Steps` for each analysis dimension. This gives us severity tiers, fix-it links, scheduled re-evaluation, and resolution tracking for free.

## ⚠️ Integration warnings

These will cause real bugs or wasted hours if ignored:

- **DO NOT `go get` cloudflare/pint.** All its check logic is under `internal/` packages — Go will refuse to compile it as a dependency. Fork the repo and copy the specific files from `internal/checks/promql/*` into our codebase (~200–500 lines each, self-contained, depend only on Prometheus parser). Alternatively, wrap `pint lint` as a subprocess by generating synthetic Prometheus rule YAML from extracted expressions.
- **DO NOT import `github.com/grafana/mimir` or any AGPL-licensed code.** AGPL is viral — it would force our entire project to be AGPL. The metric-extraction logic we want from mimirtool is ~200 lines wrapping the Prometheus parser. Reimplement it.
- **grafana-advisor-app's `checks.Check` interface** is the hook point for the Grafana App Plugin (Phase 2). Don't build a custom plugin framework — implement `Check` with `Items()` returning dashboards from the Grafana API and `Steps` for each analysis category.

## ⚠️ Technical landmines

- **Grafana JSON schema varies across versions.** Dashboard JSON from Grafana 10, 11, and 12 has structural differences (panel schema, variable model). Use `grafana-foundation-sdk` types where possible since they're auto-generated from Grafana's own schemas. Test against all three versions.
- **PromQL dialect extensions.** Thanos adds constructs (e.g., deduplication hints) that the standard Prometheus parser may not handle. When `parser.ParseExpr()` returns an error, **log the expression and skip it** — never crash the entire analysis because one query is unparseable.
- **Template-variable expansion mismatch.** Thanos slow-query logs contain the *expanded* query (`http_requests_total{job="api-server"}`), but dashboard JSON has the *templated* version (`http_requests_total{job=~"$job"}`). The reverse-mapper (Phase 3) must normalize both sides: strip `$variable` interpolations, replace with wildcard matchers, canonicalize whitespace and label order, then fuzzy-match. This is the hardest correlation problem in the project.
- **Grafana `repeat` panel expansion.** Panels with `repeat` set are stored as a single panel in JSON but rendered as N panels at runtime (one per variable value). The JSON does not contain the expanded panels — you must calculate `repeat_count = len(variable_values)` yourself to detect D2/D3.
- **Grafana variable `qryType` is version-dependent.** The structured variable query editor (`qryType: 0-5`) maps to different behaviors across Grafana versions and often sends queries to unexpected endpoints (e.g., `/api/v1/series` instead of `/api/v1/query`). For raw PromQL variable queries, use a **plain string** `query` field with `query_result(...)` wrapper — this uses Grafana's classic query mode which is stable across versions. Never use `qryType: 0` (label_names) or `qryType: 3` (query_result) for full PromQL expressions — they don't work reliably.
- **`query_result()` returns formatted strings, not raw values.** Grafana's `query_result()` returns strings like `{instance="foo:9090"} 1`, not just `foo:9090`. A `regex` field on the variable is required to extract the actual label value (e.g., `/.*"(instance-[^"]+)".*/`).
- **Prometheus `honor_labels: true` is required when exporters set their own `job`/`instance` labels.** Without it, Prometheus overwrites the exporter's labels with the scrape job name and target address. Every metric ends up with `job="exporter"` instead of the intended values.
- **Thanos sidecar requires `external_labels` on Prometheus.** It refuses to start without them. Add at least `cluster` and `replica` labels to `global.external_labels` in prometheus.yml.
- **Prometheus auto-generates `up` metrics per scrape target.** Even with `honor_labels: true`, Prometheus creates its own `up{instance="exporter:9099"}` alongside any `up` metrics the exporter emits. Variable queries on `up` will include this extra instance. Filter with regex if needed.
- **Demo anti-pattern panels must be bad-but-functional.** The slow-by-design dashboard is intentionally bad for the analyzer, but each panel must still render data in Grafana for the demo to be useful. Exception: panels demonstrating syntactically invalid PromQL (like Q10's `rate(sum(...)[5m])`) — label these clearly as broken by design.

## Project structure

```
dashboard-advisor/
├── CLAUDE.md                    # this file — coding standards, conventions, quick reference
├── ARCHITECTURE.md              # technical reference — data types, pipeline, heuristics, roadmap
├── CHANGELOG.md                 # project history — completed phases, bug post-mortems, lessons learned
├── docs/
│   └── RESEARCH.md              # full research report — competitor analysis, OSS landscape, heuristic rationale
├── docker-compose.yml           # demo stack: Prometheus + Thanos + Grafana + exporter
├── demo/
│   ├── dashboards/
│   │   ├── slow-by-design.json  # deliberately bad dashboard (test fixture)
│   │   └── fixed-by-advisor.json # corrected version (expected output of --fix)
│   ├── exporter/                # synthetic high-cardinality metric exporter
│   ├── prometheus/              # prometheus.yml config
│   ├── thanos/                  # thanos querier config
│   └── grafana/                 # grafana provisioning (datasources, dashboards)
├── pkg/
│   ├── analyzer/                # core analysis engine
│   │   ├── engine.go            # orchestrates all analyzers
│   │   ├── json_analyzer.go     # dashboard-level checks (D1-D10)
│   │   ├── promql_analyzer.go   # PromQL AST checks (Q1-Q14)
│   │   ├── cost_visitor.go      # CostVisitor for query cost estimation
│   │   └── scoring.go           # composite health score calculation
│   ├── cardinality/             # TSDB status API client (Phase 2)
│   │   ├── types.go             # CardinalityData struct + helper methods
│   │   ├── client.go            # HTTP client with 5-min TTL cache
│   │   └── client_test.go       # tests with httptest mock server
│   ├── rules/                   # individual detection rules
│   │   ├── rule.go              # Rule interface + Finding struct
│   │   ├── q1_missing_filters.go
│   │   ├── d1_too_many_panels.go
│   │   ├── b1_no_query_frontend.go
│   │   └── ...                  # one file per rule (Q1-Q12, D1-D10, B1-B7)
│   ├── extractor/               # dashboard JSON → panels/targets/variables
│   ├── fixer/                   # JSON patch generator (--fix mode)
│   └── output/                  # formatters: JSON, text, SARIF
├── cmd/
│   └── dashboard-advisor/       # CLI entrypoint
│       └── main.go
├── web/                         # React web UI (Phase 1, week 5-6)
└── plugin/                      # Grafana App Plugin (Phase 2)
```

## Rule taxonomy

Rules are identified by stable IDs. Each rule produces a `Finding` with: rule ID, severity, affected panel IDs, explanation (why), fix (what to change), expected impact, and whether it's auto-fixable.

### PromQL rules (Q-series)
- Q1: Missing label filters (bare metric, ≤1 matcher) — Critical
- Q2: Unbounded regex (`=~".*foo.*"`) — High
- Q3: Regex where equality suffices (`=~"exact"`) — Medium, auto-fixable
- Q4: High-cardinality grouping (>3 dims in `by()`) — High
- Q5: Late aggregation (aggregation wraps unfiltered expr) — Medium-High
- Q6: Long rate() ranges (>10m) — Medium-High
- Q7: Hardcoded interval instead of `$__rate_interval` — Medium, auto-fixable
- Q8: Subquery abuse (nested or fine-resolution) — High
- Q9: Duplicate expressions across panels (>2 panels) — High
- Q10: Incorrect aggregation order (`rate(sum(...))`) — Medium
- Q11: rate()/irate() on gauge metrics — Medium (needs metric type metadata)
- Q12: Impossible vector matching (no explicit label lists) — Medium
- Q13: label_replace/label_join in dashboard queries — Low-Medium
- Q14: Fragile selectors matching no current series — Medium (needs live Prometheus)

### Dashboard design rules (D-series)
- D1: Too many panels (>25 visible) — High
- D2: Repeat panels with "All" on high-cardinality variable — Critical
- D3: Template-variable explosion (chained high-cardinality vars) — Critical
- D4: Expensive variable queries (full PromQL instead of label_values) — High
- D5: Refresh <30s on complex dashboards — Medium-High, auto-fixable
- D6: Default time range >24h — Medium-High, auto-fixable
- D7: No maxDataPoints/interval set — Medium, auto-fixable
- D8: Duplicate queries across panels — Medium
- D9: Datasource mixing (>2 distinct datasources) — Low-Medium
- D10: No collapsed rows — Medium

### Backend rules (B-series) — implemented in Phase 2 weeks 7-8
- B1: No Thanos query-frontend — Critical (static inference from datasource UIDs)
- B2: Query-frontend cache misconfigured — High (stub, requires live endpoint)
- B3: No slow query logging enabled — Medium (stub, requires live endpoint)
- B4: Store gateway without external cache — High (stub, requires live endpoint)
- B5: Thanos deduplication overhead — Medium (static inference when Thanos datasource detected)
- B6: High cardinality (>1M head series) — High (requires `--prometheus-url` for live cardinality data)
- B7: Prometheus query log not enabled — Medium (stub, requires live endpoint)

## Scoring

```
score = round(100 × k / (penalty + k))    where penalty = Σ(severity_weight), k = 100
```
Severity weights: Critical = 15, High = 10, Medium = 5, Low = 2.

Uses an asymptotic formula instead of linear subtraction. Properties:
- 0 penalty → score 100 (perfect)
- penalty = k (100) → score 50 (midpoint: ~10 High findings or ~7 Critical)
- Score approaches 0 but never reaches it — every fix always moves the needle
- No clamping needed; the formula naturally stays in (0, 100]

**Why not linear?** The old `100 − penalty` formula clamped to 0, hiding progress. A dashboard with 92 findings scored 0, and after `--fix` removed 50 findings it still scored 0 — no visible improvement. The asymptotic formula ensures incremental fixes are always reflected in the score (e.g., 12 → 17 after auto-fix).

## Demo dashboard mapping

The `slow-by-design.json` dashboard is the primary test fixture. Every rule must have at least one panel triggering it. See ARCHITECTURE.md §8 for the full panel-to-rule mapping table.

## Phase overview

- **Phase 1 (weeks 1–6)**: Demo stack → analysis engine → CLI → web UI. Static analysis only (Layer 1). **COMPLETE** — 20 rules (Q1-Q10, D1-D10), 92 findings on slow dashboard, score 12.
- **Phase 2 (weeks 7–10)**: Live cardinality enrichment via TSDB status API → CostVisitor → Thanos checks → Grafana App Plugin.
  - **Weeks 7–8: COMPLETE** — TSDB client (`pkg/cardinality/`), CostVisitor, B1-B7 rules, Q11-Q12 ported from pint, CLI flags (`--prometheus-url`, `--timeout`). 96 findings on slow dashboard, score 11.
  - Weeks 9–10: Pending — Grafana App Plugin.
- **Phase 3 (weeks 11–16)**: Runtime telemetry correlation → recording-rule generation → trend tracking.

## Key conventions

- One rule per file in `pkg/rules/`
- Every rule must have a test case in `slow-by-design.json`
- Rule IDs (Q1, D1, B1) are stable — never renumber
- `Finding` struct is the universal output format for all rules
- Auto-fixable rules must implement the `Fixer` interface
- CLI exit code = 0 (no issues above threshold), 1 (issues found), 2 (error)
- Output formats: `--format=json|text|sarif`

## Design standards

1. **Test with N > 1.** State and logic bugs often only manifest with 2+ dashboards, 2+ panels, or 2+ variables. Never validate a rule against a single panel only.
2. **Test data correctness, not just status codes.** Assert on actual finding counts, rule IDs, severity levels, and affected panel IDs — not just "analysis succeeded."
3. **Design for edge cases from the start.** Handle: dashboards with zero panels, panels with no targets, unparseable PromQL, variables with no values, empty repeat configurations.
4. **Never swallow errors silently.** Log and continue — a swallowed error in the extractor causes a confusing false negative in the rule engine. If `parser.ParseExpr()` fails, log the expression and panel ID, skip it, and continue.
5. **Apply patterns symmetrically.** When adding a new field to `Finding`, update all output formatters (JSON, text, SARIF). When adding a new rule, add its test case to both demo dashboards.
6. **Return structured errors.** CLI and API errors should include which dashboard/panel failed and why, not just "analysis failed."
7. **Decouple detection from presentation.** Rules produce `Finding` structs. Formatters render them. Rules never know about output format. Formatters never know about PromQL.

## Test strategy

Tests load dashboard JSON from the demo fixtures and run rules against them. This validates our detection logic but not external system behavior.

- **What tests validate**: Our rules detect what we wrote them to detect. Scoring math is correct. Output formatters produce valid JSON/SARIF. The CLI parses flags correctly. Auto-fix produces valid dashboard JSON.
- **What tests cannot validate**: Whether our PromQL AST patterns actually match real-world anti-patterns in production dashboards. Whether the TSDB status API returns data in the format we expect. Whether Grafana's panel repeat expansion works the way we assume.
- **Every mock encodes an assumption.** If we mock the TSDB status API response, we're testing against our understanding of the API, not the API itself. When adding Phase 2 features, document behavioral assumptions in code comments.
- **The demo dashboard IS the test corpus.** `slow-by-design.json` must trigger every rule. `fixed-by-advisor.json` must trigger zero rules. If a test needs a scenario not in the demo dashboards, add it to them — don't create separate fixtures.
- **Regression tests for every bug.** When a bug is found, add a test that reproduces it before fixing. Format: `TestBug_<short_description>`.

## Bug investigation methodology

When something doesn't work as expected, follow this sequence:

1. **Characterize the symptom precisely.** What exactly is wrong? Which specific finding is missing or incorrect? What IS correct? Write it down before touching code.

2. **Look at the pattern.** The shape of what's wrong tells you the category of bug:
   - Rule finds nothing: Is the PromQL parsed? Is the AST node type what you expect? Is the panel being extracted?
   - Rule finds too much: Is the heuristic too broad? Are you matching nodes you shouldn't?
   - Wrong severity/score: Check the weight calculation. Is the rule returning the right severity?
   - Works on demo but not real dashboards: Schema difference (Grafana version), or an assumption about JSON structure.

3. **List ALL possible causes before investigating any.** For "rule Q1 not firing": PromQL parse error (skipped), panel type filtered out, target expression empty, matcher count logic wrong, wrong AST node type, panel inside collapsed row being excluded.

4. **Eliminate causes using the pattern from step 2.** The pattern should rule out most causes immediately.

5. **Distinguish code bugs from mental model bugs.** If the code does exactly what you wrote but the result is wrong, the bug is in your understanding of the external system (Grafana JSON schema, PromQL parser behavior, Thanos API) — not in the code. Verify your assumptions.

**Anti-pattern**: Assuming the first hypothesis is correct and iterating on fixes without disproving alternatives.

## Git workflow

### Branch strategy
- `main` — stable, working code
- Feature branches: `feat/<short-name>` (e.g., `feat/q4-high-cardinality-grouping`)
- Bug fixes: `fix/<short-name>` (e.g., `fix/repeat-panel-count`)
- No long-lived branches — merge and delete

### When to commit
- After completing a logical unit of work — one rule, one formatter, one CLI flag
- Before starting something risky — commit working state first
- Not in the middle — don't commit half-done rules or broken tests
- Typical granularity: 1 rule = 1 commit (implementation + test)

### Commit messages
```
<type>: <what changed> (concise, imperative mood)

<optional body — why, not what>
```
Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`

Examples:
- `feat: implement Q3 regex-as-equality detection + auto-fix`
- `fix: handle nil targets in panel extraction`
- `test: add D2 repeat-with-All regression test`
- `docs: update CHANGELOG with Phase 1 week 2 completion`

## Claude Code skills (available in this environment)

### Babysitter — workflow orchestration

The **babysitter** plugin (a5c-ai/babysitter v4.0.136) adds persistent, resumable workflow orchestration to Claude Code sessions. It prevents Claude from "finishing" prematurely on complex multi-step tasks by creating an event-sourced loop with quality gates, breakpoints for human review, and iterative convergence.

**Entry points** (functionally identical — `call` is a slash-command wrapper that invokes `babysit`):
- `/babysitter:call <instructions>` — slash command to start a babysitter run
- `babysitter:babysit` — the underlying skill invoked via the Skill tool

**How it works**: Interviews you on intent → researches codebase → creates a JavaScript process definition → drives an iterate-execute-post loop until all tasks complete or quality targets are met. State is journaled to `.a5c/runs/<runId>/` and is fully resumable.

**When to use it on this project**:
- **Batch rule implementation** — Orchestrate implementing multiple rules (e.g., "implement Q1–Q5 with TDD") with quality convergence: write test → implement rule → run tests → iterate until passing.
- **Phase-level milestones** — Run a full phase (e.g., "complete Phase 1 week 3: extractor + Q1–Q5 + scoring") with breakpoints for human review between milestones.
- **Cross-cutting verification** — After implementing several rules, verify: `slow-by-design.json` triggers every rule, `fixed-by-advisor.json` triggers zero, all formatters are updated.
- **Demo dashboard authoring** — Iteratively build `slow-by-design.json` to trigger all rules: generate panels → run analyzer → check coverage → add missing panels → repeat.

**When NOT to use it**: Single-rule implementation, quick bug fixes, research tasks, anything that takes < 15 minutes. The orchestration overhead isn't worth it for small tasks.

**Additional skill**: `babysitter-score` — quality scoring skill for evaluating task completeness within a babysitter run.

**Runtime state**: Stored in `.a5c/runs/` (add to `.gitignore` if not already there). Requires Node.js v18+ and `@a5c-ai/babysitter-sdk`.

## Doc maintenance

After completing work, update docs to stay in sync:

- **CHANGELOG.md** — Add an entry for any completed rule, bug fix, or milestone. Use the bug post-mortem format for bugs.
- **ARCHITECTURE.md** — Update if project structure, data types, or analysis pipeline changed.
- **This file** — Update if new coding standards, gotchas, or integration warnings were discovered.
- **Demo dashboards** — If a new rule was added, verify both `slow-by-design.json` (triggers it) and `fixed-by-advisor.json` (doesn't trigger it) are updated.
- **docs/RESEARCH.md** — Rarely updated. Consult it when you need the rationale behind a decision (why we chose the Prometheus parser over metricsql, why pint can't be imported, what Datadog's recommendation UX looks like). Update only if new research invalidates existing conclusions.

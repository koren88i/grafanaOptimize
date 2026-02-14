# Dashboard Performance Advisor for Grafana + Thanos — v2

**No off-the-shelf tool today combines dashboard-level PromQL extraction, Thanos-aware cost estimation, and actionable fix generation.** The pieces exist — Grafana's `dashboard-linter` provides a rule engine and JSON parser, Cloudflare's `pint` offers deep PromQL cost checking, and Prometheus's parser library is the standard AST toolkit — but nobody has assembled them into an advisor that explains *why* a dashboard is slow and *what to change*. The `grafana-advisor-app` plugin (discovered in the second research pass) provides a production-ready UX framework with `Check`/`Step` interfaces and severity tiers already built for presenting recommendations inside Grafana. Datadog's DBM Recommendations tab proves the UX pattern works commercially. The opportunity is clear and the building blocks are available.

This is the merged v2 report incorporating findings from both research passes.

---

## 1. Executive summary

- **No existing tool** performs end-to-end dashboard performance analysis for Grafana + PromQL + Thanos. The closest projects each cover only one slice.
- **`grafana/grafana-advisor-app`** (8 stars, active, ships with Grafana 12) is the ideal UX shell — it provides `checks.Check` + `checks.Step` interfaces, K8s-style API, two-tier severity model ("Action needed" / "Investigation recommended"), fix-it links, scheduled re-evaluation, and optional LLM suggestions. Build our `DashboardPerformanceCheck` on top of it.
- **`grafana/dashboard-linter`** (~312 stars) is the best JSON analysis foundation — its Go rule engine provides `DashboardRuleFunc`, `PanelRuleFunc`, `TargetRuleFunc` hooks, `--fix` auto-remediation, and `.lint` config exclusions. It already has 17 rules but none for performance.
- **`cloudflare/pint`** (~1,000 stars) has the deepest PromQL static analysis with 31 checks including `query/cost`, `promql/regexp`, `promql/selector`, `promql/impossible`, `promql/vector_matching`, and `promql/rate`. **Critical caveat**: all code is under `internal/` — cannot import as a Go library. Must fork or wrap as CLI subprocess with synthetic rule YAML.
- **MVP is demo-driven**: Week 1 ships a `docker-compose up` stack with a deliberately slow Grafana dashboard embedding every detectable anti-pattern (~30 panels, 15+ rule violations), a synthetic high-cardinality exporter (~5,000 series), Prometheus, and Thanos without query-frontend. Every rule added produces a visible before/after against this dashboard. Stakeholders can try the product at the end of any week.
- **Six capabilities must be built in-house** (no OSS exists): (1) static PromQL cost estimator, (2) template-variable explosion detector, (3) panel-to-query reverse mapper for telemetry correlation, (4) recording-rule suggestion engine, (5) weighted composite scoring, (6) dashboard JSON patch generator with RFC 6902 output.
- **Three integration paths** are recommended in parallel: Path A (dashboard lint + advisor rules), Path B (PromQL AST + analyzer + scoring), Path C (Thanos query telemetry + panel correlation).
- **`mimirtool`'s** AGPL-3.0 license means its metric-extraction logic (~200 lines) should be reimplemented, not imported.
- **VictoriaMetrics/metricsql** (241 stars) provides a zero-dependency alternative PromQL parser if MetricsQL compatibility is needed.
- **`slok/sloth`** (2,200 stars) provides the best pattern for programmatic recording-rule generation including multi-window optimization.
- **Thanos EXPLAIN proposal** (issue #5911) describes the ideal query cost output format but remains unimplemented — our `CostVisitor` fills this gap.
- **Grafana Git Sync** (Grafana 12) and **dashboard-linter CI integration** (PR #97199 in grafana/grafana) create a natural CI/CD insertion point.

---

## 2. What exists today — feature/pattern catalog

### 2.1 Grafana native tooling

| Feature | What it offers | Signals used | How presented | Gaps |
|---------|---------------|-------------|---------------|------|
| **Panel Inspector** (Stats tab) | Per-panel query execution time, rows returned, query count, request size | HTTP response timing; data frame metadata | Modal overlay per panel; 4 tabs (Stats, Data, Query, JSON) | No batch API; no recommendation engine; manual per-panel only |
| **Panel Inspector** (Query tab) | Raw HTTP request/response JSON for the data source query | Full request payload including interpolated variables | JSON tree view | No diff or comparison mode |
| **Internal metrics** (`/metrics`) | `grafana_api_dataproxy_request_all_milliseconds`, `grafana_api_dashboard_get_milliseconds`, HTTP request duration histograms by endpoint | Prometheus metrics from Grafana's own instrumentation | Scrapeable endpoint; companion dashboard (ID 20138) | Aggregate metrics only; no per-dashboard or per-panel attribution |
| **OpenTelemetry tracing** | Full distributed traces for API endpoints | Trace spans with timing, query details | OTel-compatible backends (Tempo, Jaeger) | Requires tracing infrastructure setup |
| **Dashboard HTTP API** | `/api/dashboards/uid/:uid` returns complete JSON model; `/api/search` lists all dashboards; new K8s-style API with pagination | JSON fields: `refresh`, `time.from/to`, `panels[].targets[]`, `panels[].maxDataPoints`, `panels[].interval`, `panels[].repeat`, `templating.list[]` | REST API (JSON) | No analysis or scoring; raw data only |
| **Dashboard-as-code tooling** | Git Sync (Grafana 12), Grizzly, Terraform provider, Grafonnet/Jsonnet | Dashboard JSON in version control | Git diffs, PRs | No performance lint rules in any of them |
| **Official optimization blog** | 8 concrete recommendations (label selectors, range windows, consolidate queries, recording rules, refresh frequency, shared queries, maxDataPoints) | N/A (guidance doc) | Blog post (Jan 2026) | Not automated; no detection or scoring |
| **App Plugin framework** | Custom pages under `/a/<plugin-id>/`, bundled data sources, backend Go components, RBAC, service accounts | Full Grafana API access from plugin backend | Custom React UI inside Grafana chrome | Cannot intercept queries in-flight |
| **`grafana-advisor-app`** | Scheduled checks with `Check`/`Step` interfaces, severity tiers, fix-it links, resolution tracking, optional LLM suggestions, K8s-style API, wire DI for Grafana services | Enumerable `Items()` (dashboards, datasources, plugins), `Steps` per analysis dimension | Two-tier severity cards ("Action needed" / "Investigation recommended") with resolution links | Only ships datasource/plugin/config checks — **no dashboard performance checks yet** |

### 2.2 Thanos query acceleration and introspection

| Feature | What it offers | Signals / config | Gaps |
|---------|---------------|-----------------|------|
| **Query-frontend: time splitting** | Splits long range queries into sub-queries by interval (`--query-range.split-interval=24h`) | Reduces per-query data scan; enables parallel execution | Must be deployed and configured; many orgs skip it |
| **Query-frontend: vertical sharding** | Shards queries by label values (`--query-frontend.vertical-shards=N`) | Parallelizes high-cardinality queries | Basic compared to Mimir's adaptive sharding |
| **Query-frontend: results cache** | In-memory, Memcached, or Redis backends; `--query-range.align-range-with-step` improves hit rate | `thanos_query_frontend_queries_total{result="hit\|miss"}` | Cache misconfiguration is common (0% hit rate) |
| **Slow query logging** | `--query-frontend.log-queries-longer-than=<duration>`; `--query-frontend.slow-query-logs-user-header=X-Grafana-User` | JSON logs with query expression, duration, user | Default is 0 (disabled); most orgs don't enable |
| **Store Gateway caching** | Index cache (in-memory/Memcached/Redis), caching bucket (chunks + metadata), hedged requests, Groupcache dedup | `thanos_store_index_cache_hits_total`, `thanos_objstore_bucket_operation_duration_seconds_bucket` | External Memcached required for production; in-memory defaults are too small |
| **Prometheus `query_log_file`** | JSON query log with expression, duration, samples scanned | File-based log; parseable by `chronosphereio/high-cardinality-analyzer` | Separate from Thanos logs; needs correlation |
| **TSDB status API** | `/api/v1/status/tsdb`: `seriesCountByMetricName`, `labelValueCountByLabelName`, `seriesCountByLabelPair` | Head block cardinality snapshot | Prometheus-only; Thanos federates via Store API, not TSDB status |
| **Thanos EXPLAIN proposal** (#5911) | Proposed `EXPLAIN`-style output for PromQL queries showing execution plan and cost | Unimplemented — describes ideal format for cost estimation | Not available; our CostVisitor must approximate |

### 2.3 Grafana Mimir comparison

| Capability | Thanos | Mimir |
|---|---|---|
| Time splitting | `--query-range.split-interval` | `split_queries_by_interval` |
| Query sharding | Basic `vertical-shards` | **Cardinality-aware adaptive** (`query_sharding_target_series_per_shard=2500`) |
| Results cache | In-memory / Memcached / Redis | Memcached / Redis |
| Query scheduler + fair queuing | ❌ | ✅ Per-tenant isolation (`max_outstanding_per_tenant`) |
| Slow query logging | `log-queries-longer-than` | `log_queries_longer_than` |
| Instant query splitting | ❌ | ✅ Experimental |
| Cardinality HTTP API | ❌ (via Prometheus TSDB status) | ✅ Dedicated `/api/v1/cardinality/*` endpoints |
| Metadata cache | ❌ | ✅ Fourth dedicated cache layer |

### 2.4 Datadog and competitor patterns

| Product/Feature | Pattern | Signals used | How recommendations are presented | What we should adopt |
|---|---|---|---|---|
| **Datadog DBM Recommendations** | Severity-ranked cards with evidence, actionable SQL/index fixes, mute/resolve workflow | Query plan analysis, wait events, lock contention, index usage stats | Dedicated tab + inline in query detail panels (dual placement) | Severity labels, evidence graphs, ready-to-use fix snippets, dual-placement pattern, noise management |
| **Datadog Watchdog Insights** | Zero-configuration contextual carousel; pink pill overlays on affected resources | Anomaly detection across APM, logs, RUM, DB monitoring | Embedded carousel filtered to current search/time frame across all Explorer views | Zero-config detection; contextual embedding everywhere users already work |
| **Datadog Metrics Without Limits** | Data-driven cardinality management with before/after previews | All user interactions with metrics; tag usage analysis | Cardinality estimator, tag allowlist recommendations, dashboard/monitor cross-references | Cardinality estimator with impact preview; cross-referencing which dashboards use which metrics |
| **New Relic Compute Optimizer** | Quantified savings table with ready-to-use optimized queries | Query execution costs (CCU), resource consumption patterns | Table of recommendations with estimated CCU savings per optimization | Quantified impact estimates in concrete units |
| **Splunk AI Assistant for SPL** | Agentic AI query rewriting achieving 8s avg improvement | Query execution profiles, SPL syntax analysis | Interactive assistant with before/after query comparison | AI-assisted query rewriting (future phase) |

**Design principles to adopt across all UX**: severity-driven prioritization, evidence-based recommendations with supporting data, actionable fixes (not just problem identification), contextual integration where users already work, noise management (mute/resolve), progressive disclosure (summary → detail → investigation), zero-configuration detection, quantified impact estimates.

---

## 3. Open-source landscape — complete tool matrix

Each repo scored 0–5 on five dimensions: **G**rafana relevance, **P**romQL/Thanos relevance, **M**aintainability, **I**ntegratability, **L**icense friendliness. Bucket: **D** = Direct match, **R** = Reusable component, **U** = UX inspiration, **N** = Near miss.

| # | Repo | Stars | Lang | License | Bucket | Σ/25 | Core idea | What we reuse | Key gap | Integration strategy |
|---|------|-------|------|---------|--------|------|-----------|--------------|---------|---------------------|
| 1 | **grafana/dashboard-linter** | 312 | Go | Apache-2.0 | **D** | 21 | 17 dashboard JSON lint rules; panel/target/dashboard rule hooks; `--fix` pipeline | Import `lint` package; extend with `NewTargetRuleFunc()`, `NewPanelRuleFunc()`, `NewDashboardRuleFunc()`; use `.lint` config exclusions and `ResultSet.AutoFix()` | No performance/cardinality checks; no scoring | **Low** — direct Go import |
| 2 | **grafana/grafana-advisor-app** | 8 | Go+TS | Apache-2.0 | **U** | 19 | Grafana plugin: `Check`/`Step` interfaces, severity tiers, fix-it links, scheduled checks, LLM suggestions | Implement `checks.Check` for `DashboardPerformanceCheck`; use `Items()` for dashboard enumeration; define `Steps` per analysis dimension; leverage scheduled re-evaluation + resolution tracking | Only ships datasource/plugin/config checks — no dashboard perf | **Med** — implement Check interface |
| 3 | **cloudflare/pint** | ~1,000 | Go | Apache-2.0 | **R** | 18 | 31 Prometheus-rule checks incl. `query/cost`, `promql/regexp`, `promql/selector`, `promql/impossible`, `promql/vector_matching`, `promql/rate` | Deep anti-pattern detection logic (regex misuse, missing filters, impossible joins, counter misuse) + live cost estimation | `internal/` packages — **cannot import as library**; operates on rule YAML, not dashboard JSON | **High** — fork and lift `internal/checks/promql/*` (~200-500 lines each); or wrap `pint lint` as subprocess with synthetic rule YAML |
| 4 | **prometheus/prometheus** (`promql/parser`) | 57,000+ | Go | Apache-2.0 | **R** | 22 | Canonical PromQL parser: AST, `Walk()`/`Inspect()` visitors, prettify, type checking | `parser.ParseExpr()` → `Walk(visitor, node)` → inspect `*VectorSelector.LabelMatchers`, `*Call.Func.Name`, `*AggregateExpr`, `*BinaryExpr.VectorMatching`, `*SubqueryExpr` | Pure parser — no lint rules, no cost estimation | **Low** — direct Go import |
| 5 | **prometheus/promlens** | 663 | Go+TS | Apache-2.0 | **U** | 18 | Web-based PromQL tree visualizer; sub-expression execution with series counts; form-based builder | Tree-view React components for query structure display; warning-hint annotation model on AST nodes; metrics explorer UI | Standalone app, not library; backend tightly coupled to its own server | **Med** — borrow React components and annotation UX model |
| 6 | **grafana/mimir** (mimirtool) | 4,300 | Go | **AGPL-3.0** ⚠️ | **R** | 17 | `analyze grafana` extracts metric names from all dashboards; `analyze prometheus` finds used vs. unused metrics | Dashboard-to-metric-name extraction pattern; Grafana API enumeration logic | **AGPL license (viral)**; extracts only metric names, not full query analysis; CLI-only | **Med** — **reimplement** the extraction pattern (~200 lines); **do not import AGPL code directly** |
| 7 | **grafana/grafana-foundation-sdk** | 201 | Multi | Apache-2.0 | **R** | 19 | Multi-language typed dashboard SDK auto-generated from Grafana schemas; builder pattern | Authoritative Go/TS/Python types for dashboard JSON; stays in sync with Grafana releases; schema-accurate field coverage | No analysis logic; heavier dependency than needed for parsing | **Low** — use Go types for deserialization |
| 8 | **grafana-tools/sdk** | ~400 | Go | Apache-2.0 | **R** | 18 | Go SDK mapping Grafana objects (Board, Panel, Row, Target, Datasource) to structs; includes API client | `sdk.NewClient()` for API access; `Board.Panels[].Targets[].Expr` path for PromQL extraction; lightweight alternative to foundation-sdk | Types may lag Grafana releases; not auto-generated | **Low** — direct Go import |
| 9 | **VictoriaMetrics/metricsql** | 241 | Go | Apache-2.0 | **R** | 19 | Standalone PromQL + MetricsQL parser; zero dependencies; constant folding; WITH-expression expansion | Drop-in parser if MetricsQL compat needed; simpler dependency tree than full Prometheus import; `optimizer.go` patterns | MetricsQL extensions may cause false positives on standard PromQL | **Low** — `metricsql.Parse()` → type-assert AST nodes |
| 10 | **slok/sloth** | 2,200 | Go | Apache-2.0 | **U** | 18 | Generates optimized Prometheus recording + alerting rules from SLO definitions | `OptimizedSLIRecordingRulesGenerator` pattern for multi-window recording-rule generation; programmatic rule templating | Generates from SLO specs, not dashboard queries | **Med** — borrow recording-rule template generation pattern |
| 11 | **chronosphereio/high-cardinality-analyzer** | 50 | Go | MIT | **R** | 14 | Parses Prometheus query logs → finds slow queries → auto-generates recording-rule templates | Query-log parsing for `query_log_file` JSON format; cumulative-time analysis; recording-rule generation for aggregation queries | Archived Jul 2024; only Prometheus query logs, not Thanos; no dashboard awareness | **Low** — reuse log-parsing and rule-generation logic |
| 12 | **thought-machine/prometheus-cardinality-exporter** | 43 | Go | Apache-2.0 | **R** | 17 | Scrapes `/api/v1/status/tsdb`, re-exports cardinality as Prometheus metrics | Continuous cardinality monitoring; K8s service discovery; per-metric series counts | Prometheus-only (no Thanos Store Gateway); no query analysis | **Low** — scrape its metrics or reuse TSDB status parsing code |
| 13 | **ContainerSolutions/prom-metrics-check** | 66 | Python | MIT | **N** | 16 | Scans Grafana via API, extracts PromQL from all panels, validates metric existence against Prometheus | Reference implementation for "scan all dashboards and extract queries" pattern | Python (not Go); low activity; limited to metric existence checks | **Med** — reference pattern only |
| 14 | **esnet/gdg** | ~200 | Go | BSD-3 | **N** | 16 | Bulk backup/restore/manage Grafana dashboards across instances | Bulk dashboard export infrastructure; context-based multi-instance management | No analysis capabilities; primarily backup tool | **Low** — use API client pattern if needed |
| 15 | **prometheus-community/promql-langserver** | 180 | Go | Apache-2.0 | **N** | 16 | LSP for PromQL: diagnostics, hover docs, autocompletion connected to live Prometheus | Diagnostic message patterns; function documentation lookup; metadata enrichment from live Prometheus | LSP protocol, not a lintable library; tight editor coupling | **High** — borrow diagnostic format and metadata-fetching logic |

---

## 4. Diagnosis model — signals, heuristics, and scoring

### 4.1 Three signal layers

**Layer 1 — Static analysis (dashboard JSON + PromQL AST).** Requires only dashboard JSON via Grafana API. Parse JSON to extract panel count, query expressions, variable definitions (type, refresh policy, `includeAll`, `multi`, query expression), repeat configuration, refresh interval, time range, maxDataPoints, interval, collapsed row membership. Parse each PromQL expression into an AST using the Prometheus parser. Apply pattern-matching rules via `parser.Walk()` custom Visitors. This layer is deterministic, fast, and works offline. It is the entire MVP.

**Layer 2 — Live cardinality enrichment.** Query Prometheus TSDB status API (`/api/v1/status/tsdb`) or Mimir cardinality API (`/api/v1/cardinality/*`) to fetch `seriesCountByMetricName`, `labelValueCountByLabelName`, `seriesCountByLabelPair`. Cross-reference with metrics and labels in dashboard queries to convert heuristic guesses into measured facts. Additionally query variable endpoints to fetch actual variable value counts for explosion detection. The TSDB status endpoint is lightweight and cacheable (5-minute TTL).

**Layer 3 — Runtime telemetry correlation.** Enable `--query-frontend.log-queries-longer-than=5s` on Thanos and `query_log_file` on Prometheus. Build a log ingestion pipeline that parses JSON logs, extracts queries with durations and sample counts, then **reverse-maps each expression to dashboard panels**. This requires a normalization layer that strips Grafana variable interpolations (`$job` → regex placeholder), canonicalizes whitespace and label order, and produces a fuzzy-match index. Output: per-panel `[query_duration, series_touched, samples_processed]`. Combine with Thanos cache-hit metrics to identify panels not benefiting from caching.

### 4.2 Six custom components to build (no OSS exists)

These represent the core value-add and cannot be assembled from existing projects:

**1. Static PromQL cost estimator (`CostVisitor`).** Walk the parse tree and assign weights based on: number of `VectorSelector` nodes × estimated series (from TSDB status API or heuristic defaults), range-vector window sizes, aggregation nesting depth, presence of `group_left`/`group_right` with high-cardinality sides, subquery step intervals, and regex matcher complexity. Inspired by the unimplemented Thanos EXPLAIN proposal (#5911). Output: estimated cost units per query, comparable across panels.

**2. Template-variable explosion detector.** Analyze the interaction between Grafana template variables, their cardinality, `includeAll` flags, `multi` selection, and `repeat` directives on panels/rows. Logic: fetch variable value counts from datasource (or estimate from TSDB status), multiply across chained variables, and flag dashboards where `(var1_values × var2_values × repeated_panels) > threshold` would produce hundreds of concurrent queries on load. This is the single most common cause of "dashboard hangs when I select All."

**3. Panel-to-query reverse mapper.** Thanos logs raw PromQL with variables already expanded (e.g., `http_requests_total{job="api-server"}`), while dashboard JSON contains templated expressions (`http_requests_total{job=~"$job"}`). Normalization layer: strip `$variable` interpolations, replace with wildcard matchers, canonicalize whitespace/label order, produce similarity hashes. Build an inverted index from normalized expression → `[dashboard_uid, panel_id]` for O(1) correlation.

**4. Recording-rule suggestion engine.** Identify expensive aggregation patterns appearing in multiple panels or dashboards (deduplicated by normalized expression hash). Generate recording-rule YAML with appropriate `interval`, label preservation, and `level:metric:operations` naming convention (following sloth's multi-window pattern). Estimate cost savings: `(query_count × avg_duration) - (recording_rule_eval_cost)`.

**5. Weighted composite scoring.** No OSS tool produces a dashboard health score. Framework: weight structural issues (panel count, missing units = low), query issues (regex abuse, missing filters = medium-high), performance issues (cardinality, range windows = high), and operational issues (cache miss rates, execution times = critical) into a 0–100 score. Tunable per-org thresholds. Score calculation: `round(100 × k / (penalty + k))` where `penalty = Σ(severity_weight)` and `k = 100`. Uses asymptotic formula so every fix visibly improves the score (no clamping to 0). Severity weights: Critical = 15, High = 10, Medium = 5, Low = 2.

**6. Dashboard JSON patch generator.** Extend dashboard-linter's `--fix` with richer patching: replace hardcoded intervals with `$__rate_interval`, add missing label matchers, convert `=~"value"` to `="value"`, insert `$__interval` / `minInterval`, adjust refresh, set maxDataPoints. Output RFC 6902 JSON Patches suitable for PR review in Git Sync workflows or automated application via Grafana API.

### 4.3 Detection heuristics — complete rule set

#### PromQL query anti-patterns

| # | Issue pattern | Detection heuristic | Recommended fix | Expected impact | Risk / tradeoff |
|---|---|---|---|---|---|
| Q1 | **Missing label filters** — bare metric with ≤1 matcher | Count matchers per `VectorSelector` in AST; flag if only `__name__` | Add scoping labels (`job`, `namespace`, `cluster`) | **Critical** — 10–100× series reduction | Requires knowledge of available labels |
| Q2 | **Unbounded regex** — `=~".*foo.*"` or `=~".+"` | Detect `.*`, `.+` in regex matchers without anchored literal prefix | Replace with equality or anchored alternation (`=~"GET\|POST"`) | **High** — eliminates full posting-list scan | May need labeling strategy changes |
| Q3 | **Regex where equality suffices** — `=~"exact_value"` | Detect regex matchers with no regex metacharacters (port pint's `promql/regexp` logic) | Convert to `="exact_value"` | **Medium** — avoids regex engine overhead; auto-fixable |  None |
| Q4 | **High-cardinality grouping** — `by(pod, container, instance)` | Flag `by()` with known high-cardinality labels or >3 group-by dimensions | Aggregate by lower-cardinality labels; use recording rules | **High** — reduces result set by orders of magnitude | Loses per-pod granularity |
| Q5 | **Late aggregation** — aggregation wraps unfiltered expression | Check if aggregation's inner expression lacks label filters | Add filters before aggregation; push aggregation closer to selector | **Medium-high** — 5–50× intermediate memory reduction | None significant |
| Q6 | **Long rate() ranges** — `rate(m[>10m])` | Parse range duration inside `rate`/`irate`/`increase`; flag >10m | Use `$__rate_interval` or 2–5m ranges; recording rules for long-window aggregations | **Medium-high** — proportional sample reduction | Smoothing effect changes |
| Q7 | **Hardcoded step/interval** — `[5m]` instead of `$__rate_interval` | Detect literal duration in rate functions where Grafana variable should be used | Replace with `$__rate_interval` | **Medium** — prevents under/over-sampling across time ranges; auto-fixable | Requires Grafana 7.2+ |
| Q8 | **Subquery abuse** — nested or fine-resolution subqueries | Detect `[range:step]` syntax; flag nested subqueries or step <1m with range >1h | Convert to recording rules; increase subquery step | **High** — eliminates O(range/step) inner evaluations | Requires recording rule infrastructure |
| Q9 | **Duplicate complex expressions** — same expression in >2 panels | Normalize and hash all query expressions; flag duplicates with >2 functions | Create recording rules; use Dashboard data source for shared queries | **High** — eliminates N-1 redundant evaluations | Recording rules add operational overhead |
| Q10 | **Incorrect aggregation order** — `rate(sum(...))` | Detect rate/increase wrapping aggregation in AST | Rewrite as `sum(rate(...))` | **Medium** — correctness fix that also improves performance | None |
| Q11 | **rate()/irate() on gauge metrics** — counter functions applied to gauges | Cross-reference metric names with TSDB metadata (metric type); port pint's `promql/rate` check | Use `avg_over_time()` or `deriv()` for gauges | **Medium** — correctness + removes misleading results | Requires metric type metadata |
| Q12 | **Impossible vector matching** — `group_left`/`group_right` without explicit label lists | Detect binary expressions with VectorMatching that lacks MatchingLabels (port pint's `promql/vector_matching`) | Add explicit `on()`/`ignoring()` label lists | **Medium** — prevents silent empty results or explosions | Requires label schema knowledge |
| Q13 | **label_replace/label_join in dashboards** | Count occurrences in query expressions | Move to relabeling in scrape config or recording rules | **Low-medium** — small per-query savings | Requires upstream config changes |
| Q14 | **Fragile selector patterns** — selectors that may silently match nothing due to label changes | Port pint's `promql/selector` logic: warn when selectors don't match any current series | Add or fix label matchers | **Medium** — prevents silently broken panels | Requires live Prometheus access |

#### Dashboard design anti-patterns

| # | Issue pattern | Detection heuristic | Recommended fix | Expected impact | Risk / tradeoff |
|---|---|---|---|---|---|
| D1 | **Too many panels** — >25 visible panels | Count panels array length excluding collapsed rows | Collapsible rows; split into focused dashboards; consolidate similar panels | **High** — proportional reduction in parallel query burst | More dashboards to maintain |
| D2 | **Repeat panels with "All"** — repeat on high-cardinality variable | Check `repeat` field + variable `includeAll: true` + `multi: true`; estimate `(var_values × repeated_panels)` | Disable "All"; use single panel with grouping; add variable regex filter | **Critical** — can reduce query count from hundreds to single digits | Users lose per-item dedicated panels |
| D3 | **Template-variable explosion** — chained high-cardinality variables | Calculate cross-product: `Π(var_i.values_count)` for all chained variables with `includeAll` | Limit variable cardinality with regex; remove `includeAll`; add variable dependency caching | **Critical** — prevents dashboard-load query storms | Reduces variable flexibility |
| D4 | **Expensive variable queries** — complex PromQL in variable definition | Parse `templating.list[].query`; flag full PromQL vs. simple `label_values()` | Use `label_values(metric, label)` which reads from index (ms vs. seconds) | **High** — unblocks dashboard initial render | Less flexible variable filtering |
| D5 | **Refresh <30s** on complex dashboards | Check `refresh` field; cross-reference with panel count and query complexity | Set refresh to 1–5 minutes; disable for historical dashboards | **Medium-high** — eliminates query pileup | Delayed real-time visibility |
| D6 | **Default time range >24h** | Parse `time.from` relative duration | Default to 1h or 6h; use panel-level `timeFrom` overrides for specific long-range panels | **Medium-high** — reduces evaluation steps and samples | Users must manually widen range |
| D7 | **No maxDataPoints/interval set** | Check for absent or default values in panel targets | Set `maxDataPoints` to 1000–1500; set `minInterval` to scrape interval | **Medium** — prevents over-fetching fine-grained data; auto-fixable | Slightly coarser graph resolution |
| D8 | **Duplicate queries across panels** | Normalize and hash all expressions; flag identical queries in different panels | Use Grafana "Dashboard" data source to share query results | **Medium** — eliminates redundant data source calls | Couples panels |
| D9 | **Datasource mixing** — panels using different datasources on same dashboard | Check for >2 distinct datasource references across panels | Separate into per-datasource dashboards or use mixed datasource intentionally | **Low-medium** — reduces cognitive load; may indicate accidental misconfiguration | May be intentional for correlation dashboards |
| D10 | **No collapsed rows** — all panels visible on load | Check if any `row` type panels have `collapsed: true` | Group related panels into collapsed rows | **Medium** — defers query execution for hidden panels | Users must click to expand |

#### Backend and architecture issues

| # | Issue pattern | Detection heuristic | Recommended fix | Expected impact | Risk / tradeoff |
|---|---|---|---|---|---|
| B1 | **No Thanos query-frontend** | Check datasource URL; query Thanos `/stores` or config | Deploy query-frontend with 24h split, Memcached cache, `align-range-with-step` | **Critical** — typically **2–10× improvement** for range queries | Adds operational component |
| B2 | **Query-frontend cache misconfigured** | Monitor `thanos_query_frontend_queries_total{result="hit"}` — 0% = broken | Verify cache backend; set `response-cache-max-freshness=1m`; set `max_size` | **High** — cache hits return in ms vs. seconds | Cache staleness for volatile data |
| B3 | **No slow query logging** | Check if `--query-frontend.log-queries-longer-than` is 0 (default) | Set to 10s; add user header for attribution | **Medium** — enables ongoing identification of expensive queries | Log volume increase |
| B4 | **Store gateway without external cache** | Check store gateway config for index cache type | Deploy external Memcached for index cache; add caching bucket | **High** — reduces object storage round-trips 5–20× | Memcached operational overhead |
| B5 | **Thanos deduplication overhead** | Check `--query.replica-label` config | Correct replica label; enable compactor-level dedup for historical data | **Medium-high** — reported 293ms → 23s with dedup on 1,200 series | Requires compactor config |
| B6 | **High cardinality** (>1M head series) | Query `prometheus_tsdb_head_series`; TSDB status API | Drop high-cardinality labels via `metric_relabel_configs`; set `sample_limit` | **High** — reduces memory, storage, query cost | Loss of label granularity |
| B7 | **Prometheus query log not enabled** | Check Prometheus config for `query_log_file` | Enable query logging for telemetry correlation (Path C) | **Medium** — enables recording-rule candidate identification | Storage for log files |

---

## 5. Proposed architecture

### 5.1 Three integration paths (build in parallel)

**Path A: Dashboard lint + advisor rules engine.** Import `grafana/dashboard-linter` lint package and extend with custom performance rules. Wrap inside `grafana-advisor-app` pattern — implement `checks.Check` interface with `Items()` returning all dashboards and `Steps` for each analysis category (panel count, query complexity, repeat explosion, variable queries, refresh/range, deduplication). Present via advisor-app's severity model with resolution links to specific panels.

**Path B: PromQL AST + analyzer + scoring.** For each `Target.Expr`, call `parser.ParseExpr()` → custom `Visitor` functions via `parser.Walk()`. Detect regex misuse (port pint's `promql/regexp`), missing filters (`promql/selector`), rate-on-gauge (`promql/rate`), impossible joins (`promql/vector_matching`), large ranges, nested subqueries, late aggregation. For cost scoring, query TSDB status API for series counts → estimate cost as `(matched_series) × (range_window / step) × (aggregation_depth)`. **Pint reuse strategy**: fork repo and lift ~10 files from `internal/checks/promql/*` into our codebase (self-contained, depend only on Prometheus parser), or wrap `pint lint` as subprocess generating synthetic rule YAML from extracted expressions.

**Path C: Thanos query telemetry + panel correlation.** Enable slow query logging. Build log ingestion pipeline. Parse JSON logs → extract expressions with durations → reverse-map to panels via normalized expression index (Path A's extraction + fuzzy matching). Combine with cache-hit metrics. Output: per-panel measured performance with recording-rule suggestions for aggregation queries exceeding cumulative time threshold.

### 5.2 Component diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                  Grafana Advisor Plugin (UI)                     │
│  grafana-advisor-app pattern: Check/Step interfaces              │
│  Severity tiers · Fix-it links · Scheduled re-evaluation         │
│  React frontend: panel-level score cards, sorted by impact       │
│  PromLens-inspired AST visualization for query drill-down        │
└────────────────────────┬────────────────────────────────────────┘
                         │ K8s-style API / REST API
┌────────────────────────▼────────────────────────────────────────┐
│               Dashboard Performance Engine (Go)                  │
│                                                                  │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │  Dashboard        │  │  PromQL           │  │  Telemetry    │  │
│  │  Scanner          │  │  Analyzer         │  │  Correlator   │  │
│  │                   │  │                   │  │               │  │
│  │ • Grafana API     │  │ • ParseExpr()     │  │ • Query log   │  │
│  │   enumeration     │  │ • AST Walk()      │  │   ingestion   │  │
│  │ • JSON → lint     │  │   with custom     │  │ • Slow-query  │  │
│  │   Dashboard types │  │   Visitors        │  │   detection   │  │
│  │ • Panel/Target/   │  │ • CostVisitor     │  │ • Panel ↔     │  │
│  │   Variable        │  │   (cost estimator)│  │   query       │  │
│  │   extraction      │  │ • Anti-pattern    │  │   reverse map │  │
│  │ • Repeat          │  │   checks (pint-   │  │ • Cache-hit   │  │
│  │   explosion       │  │   derived)        │  │   analysis    │  │
│  │   detector        │  │ • Metric type     │  │               │  │
│  │ • Variable query  │  │   resolution      │  │               │  │
│  │   analyzer        │  │                   │  │               │  │
│  └──────┬────────────┘  └──────┬────────────┘  └──────┬────────┘  │
│         │                      │                      │           │
│  ┌──────▼──────────────────────▼──────────────────────▼────────┐  │
│  │              Recommendation Engine                          │  │
│  │  • Weighted scoring (0–100 per panel, per dashboard)        │  │
│  │  • Recording-rule suggestion generator (sloth pattern)      │  │
│  │  • Dashboard JSON patch generator (RFC 6902 output)         │  │
│  │  • Diff output for PR/CI integration (SARIF format)         │  │
│  │  • Mute/resolve noise management                            │  │
│  └─────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
         │                    │                    │
   ┌─────▼─────┐     ┌───────▼──────┐    ┌───────▼──────────┐
   │ Grafana   │     │ Prometheus/  │    │ Thanos Query     │
   │ HTTP API  │     │ Thanos       │    │ Frontend Logs    │
   │           │     │ /api/v1/     │    │ + Prometheus     │
   │ Dashboards│     │ status/tsdb  │    │ query_log_file   │
   │ Folders   │     │ + query API  │    │                  │
   │ Variables │     │ + metadata   │    │                  │
   └───────────┘     └──────────────┘    └──────────────────┘
```

**Key Go dependencies** (all Apache-2.0 or MIT):
- `github.com/grafana/dashboard-linter/lint` — dashboard JSON parsing + rule engine
- `github.com/prometheus/prometheus/promql/parser` — canonical PromQL AST
- `github.com/grafana-tools/sdk` or `github.com/grafana/grafana-foundation-sdk` — Grafana API client + board types
- Custom code: anti-pattern visitors, CostVisitor, telemetry correlator, recommendation engine, JSON patch generator

### 5.3 API design

**Analysis engine REST API:**
- `POST /api/v1/analyze` — accepts dashboard JSON, returns full analysis with score and recommendations
- `GET /api/v1/dashboards` — lists all tracked dashboards with health scores and trends
- `GET /api/v1/dashboards/:uid/recommendations` — recommendations for specific dashboard
- `GET /api/v1/dashboards/:uid/history` — score history over time
- `POST /api/v1/fix` — accepts dashboard JSON + recommendation IDs, returns patched JSON (RFC 6902) or full patched dashboard
- `POST /api/v1/recording-rules` — accepts recommendation IDs, returns generated recording-rule YAML

**Grafana App Plugin backend** proxies these via Grafana's resource handler, adding RBAC and service account authentication.

**CLI:**
- `dashboard-advisor lint ./dashboards/ --format=sarif --fail-on=high`
- `dashboard-advisor lint --grafana-url=... --grafana-token=... --format=json`
- `dashboard-advisor fix ./dashboards/ --output=./patched/`
- `dashboard-advisor suggest-rules ./dashboards/ --output=recording-rules.yaml`

---

## 6. User stories

| # | User story | Persona | Supported by |
|---|---|---|---|
| US1 | As a **platform engineer**, I want to see a health score for every dashboard so I can prioritize which ones to fix first | Platform/SRE | Dashboard list view with scores; composite scoring model |
| US2 | As a **dashboard author**, I want to see specific recommendations with "why" and "how to fix" for my slow dashboard | Developer | Per-dashboard recommendation cards with evidence and fix snippets |
| US3 | As a **platform engineer**, I want to automatically block dashboards with critical issues from being merged | Platform/SRE | CLI + CI/CD gate (`--fail-on=high`); SARIF output for GitHub code scanning |
| US4 | As a **dashboard author**, I want to auto-fix simple issues like missing `maxDataPoints` or regex-where-equality-suffices | Developer | `--fix` mode; JSON patch generator; "Apply fix" button in UI |
| US5 | As a **SRE**, I want to identify which panels are causing the most load on our Thanos cluster | SRE | Runtime telemetry correlation (Path C); panel-level cost scoring |
| US6 | As a **platform engineer**, I want recording-rule YAML generated for expensive queries that appear in multiple dashboards | Platform/SRE | Recording-rule suggestion engine; export as PrometheusRule CRD |
| US7 | As a **team lead**, I want to track dashboard health over time and get alerted when a dashboard degrades | Management/Platform | Score history; trend sparklines; threshold-based alerting |
| US8 | As a **dashboard author**, I want to suppress irrelevant recommendations without losing real issues | Developer | Mute/resolve workflow; per-rule and per-dashboard suppression; `.lint` config |
| US9 | As a **platform engineer**, I want to know if our Thanos query-frontend caching is actually working | Platform/SRE | Backend check B2: cache hit-rate monitoring; recommendation to fix config |
| US10 | As a **dashboard author**, I want to understand why selecting "All" on a variable makes my dashboard hang | Developer | Template-variable explosion detector (D2/D3); visual showing `var_values × panels` |
| US11 | As a **team evaluating this tool**, I want to `docker-compose up` and immediately see a slow dashboard + the advisor diagnosing it, so I understand the value in 5 minutes | Any stakeholder | Demo stack with slow-by-design + fixed-by-advisor dashboards; advisor pre-configured to analyze them |
| US12 | As a **developer building this tool**, I want every rule to have a visible test case in the demo dashboard so I can validate my work end-to-end without mocking | Developer | slow-by-design.json contains at least one instance of every detectable anti-pattern |

---

## 7. MVP scope and phased roadmap — demo-driven development

### Philosophy: the demo dashboard *is* the first deliverable

The MVP does not end with a demo — it **starts** with one. Week 1 produces a fully running stack (docker-compose with Grafana, Prometheus, Thanos, and a synthetic workload) and a deliberately awful "slow-by-design" dashboard that embeds every anti-pattern we intend to detect. Every subsequent rule, heuristic, or UI element is validated against this dashboard. The team can `docker-compose up`, open the slow dashboard, feel the pain, then open the advisor and see the diagnosis evolve commit-by-commit.

This means:
- Every rule has a visible test case from day one — no abstract lint output without a real dashboard to look at.
- Stakeholders can try the product at the end of any week, not just at the end of a phase.
- Scoring calibration happens against a known-bad baseline, not hypothetical dashboards.
- The "fixed" version of the demo dashboard serves as a before/after proof of impact.

### The slow-by-design demo dashboard

The demo environment is a `docker-compose.yml` with: Prometheus (scraping itself + node-exporter + a high-cardinality synthetic exporter), Thanos sidecar + querier (no query-frontend, intentionally), and Grafana provisioned with two dashboards: `slow-by-design.json` and `fixed-by-advisor.json`.

The slow dashboard is a single JSON file that packs every detectable anti-pattern into ~30 panels:

| Panel(s) | Anti-pattern triggered | Rule(s) exercised |
|---|---|---|
| "Global Request Rate" | Bare `http_requests_total` with no label filters | Q1 |
| "Error Ratio" | `=~".*error.*"` unbounded regex | Q2 |
| "Status Codes" | `=~"200"` regex where equality works | Q3 |
| "Latency by Pod" | `by(pod, container, instance, namespace)` — 4-dim high-cardinality grouping | Q4 |
| "Total Rate" | `rate(sum(http_requests_total)[5m])` — aggregation inside rate | Q10 |
| "P99 over 1h" | `histogram_quantile(0.99, rate(http_request_duration_bucket[1h]))` — huge range | Q6 |
| "Smoothed CPU" | Nested subquery: `avg_over_time(rate(cpu_usage[5m])[1h:10s])` | Q8 |
| "Memory (copy 1)" through "Memory (copy 4)" | Same `process_resident_memory_bytes` expression in 4 panels | Q9 |
| "Goroutine Count" | Hardcoded `[5m]` instead of `$__rate_interval` | Q7 |
| "Requests by Pod" (repeated) | `repeat: pod` with `includeAll: true` on a 200-value variable | D2, D3 |
| All 30 panels visible | No collapsed rows, no row grouping | D1, D10 |
| Variable `$instance` | Defined as `query_result(count by(instance)(up))` — full PromQL in variable | D4 |
| Dashboard settings | `refresh: 10s`, `time.from: now-7d`, no `maxDataPoints` set | D5, D6, D7 |
| Datasource URL | Points to Thanos querier directly (no query-frontend) | B1 |

The "fixed" dashboard applies every recommendation: filters added, regex simplified, recording rules referenced, panels consolidated into collapsed rows, refresh set to 1m, range set to 1h, maxDataPoints set to 1000, repeat variable capped with regex, variable query simplified to `label_values(up, instance)`.

A synthetic metric exporter (small Go binary or Prometheus client library script) generates `http_requests_total` with ~5,000 series (across `method`, `status`, `path`, `pod`, `container`, `instance`, `namespace` labels) so cardinality-related rules produce meaningful results even in the demo.

### Phase 1 — Demo stack + analysis engine + CLI (weeks 1–6)

**Week 1: Demo environment.** Create `docker-compose.yml` with Prometheus, Thanos sidecar + querier (no query-frontend), Grafana, and synthetic exporter. Provision `slow-by-design.json` and `fixed-by-advisor.json` dashboards. Write a `README` with `docker-compose up` → open dashboard → experience slowness → run advisor. The demo environment is also the integration test harness for all future work.

**Week 2–3: Analysis engine core (first 8 rules).** Implement Go library. Import `grafana/dashboard-linter` lint package for JSON parsing + `prometheus/prometheus/promql/parser` for PromQL AST. Implement the first 8 rules that light up against the demo dashboard: Q1 (missing filters), Q2 (unbounded regex), Q3 (regex→equality), Q10 (incorrect aggregation order), D1 (too many panels), D2 (repeat+All), D5 (refresh <30s), D7 (missing maxDataPoints). Each rule produces a structured finding with severity, affected panel IDs, explanation ("why this is slow"), fix text ("what to change"), and expected impact. Generate composite health score. **Demo checkpoint**: run engine against `slow-by-design.json`, see 8+ findings; run against `fixed-by-advisor.json`, see 0.

**Week 4: CLI + remaining static rules.** Build CLI that reads JSON from files or fetches from Grafana API. Output: JSON, human-readable text, SARIF. Add `--fail-on=high|medium|low`. Add `--fix` mode for auto-fixable rules (Q3, Q7, D5, D6, D7 — generates patched JSON). Add remaining MVP rules: Q4 (high-cardinality grouping), Q5 (late aggregation), Q6 (long rate ranges), Q7 (hardcoded interval), Q8 (subqueries), Q9 (duplicates), D4 (expensive variables), D6 (range >24h). **Demo checkpoint**: `dashboard-advisor lint slow-by-design.json` prints 15+ findings with score; `dashboard-advisor fix slow-by-design.json --output patched.json` produces a dashboard that matches `fixed-by-advisor.json`.

**Week 5–6: Web UI.** React web app (standalone Docker container, added to docker-compose). Dashboard list with health scores. Dashboard detail with recommendation cards: severity badge, issue title, affected panels, "Why this is slow", "What to change" with PromQL diff, "Expected impact", "How to validate". Filter by severity. Mute/resolve controls. **Demo checkpoint**: full end-to-end flow — open slow dashboard in Grafana (tab 1), open advisor web UI (tab 2), see the diagnosis, click through recommendations, see the panel IDs link back to Grafana.

**Week 6 deliverable**: A docker-compose stack anyone can run in 2 minutes that shows: (1) a visibly slow Grafana dashboard, (2) a CLI that diagnoses it with 15+ findings, (3) a web UI that presents actionable recommendations, (4) a `--fix` command that produces the fast version. Supports US1, US2, US3, US4, US8.

### Phase 2 — Live enrichment + Grafana plugin (weeks 7–10)

**Week 7–8: Cardinality enrichment + CostVisitor.** TSDB status API integration for actual series counts. Implement CostVisitor: `cost = estimated_series × (range_seconds / step_seconds) × function_complexity_factor`. Template-variable explosion detection with live variable value counts. Add Thanos-specific checks: B1 (query-frontend presence), B2 (cache hit rates), B3 (slow query logging). Port pint's `promql/regexp`, `promql/selector`, and `promql/rate` logic (forked from `internal/`). **Demo evolution**: add Thanos query-frontend to docker-compose as optional profile (`docker-compose --profile optimized up`). Advisor now detects its absence in default profile, and confirms its presence in optimized profile. Cardinality enrichment upgrades findings from "this metric *might* be high-cardinality" to "this metric has 5,247 active series."

**Week 9–10: Grafana App Plugin.** Package as Grafana App Plugin using `@grafana/create-plugin`. Implement `checks.Check` interface from `grafana-advisor-app` pattern. Navigation entry in sidebar. Backend Go component running analysis engine. "Analyze this dashboard" contextual link. Service account auth. Test across Grafana 10.x, 11.x, 12.x. **Demo evolution**: the advisor is now *inside* Grafana — no second tab. Supports US5, US9, US10.

### Phase 3 — Runtime profiling + advanced features (weeks 11–16)

**Week 11–12: Query replay + telemetry correlation (Path C).** Replay queries via `/api/ds/query`, measure timing/series/samples. Build panel-to-query reverse mapper with normalization layer. Ingest Thanos slow query logs and Prometheus `query_log_file`. Calibrate scoring model against measured timing. **Demo evolution**: advisor now shows measured query times next to each panel ("Panel 7: avg 4.2s, 120k series touched") and highlights the gap between estimated and actual cost.

**Week 13–14: Recording-rule generation.** Identify recording-rule candidates (duplicated expensive aggregation patterns across demo dashboard). Generate YAML following `level:metric:operations` naming. Export as PrometheusRule CRD. Estimate cost savings per suggested rule. **Demo evolution**: advisor generates `recording-rules.yaml`, user applies it to Prometheus, re-runs advisor — score improves. Supports US6.

**Week 15–16: Trend tracking + alerting.** Store analysis results over time. Score trend visualization. Alert on score drops. Grafana alerting integration. **Demo evolution**: demo includes a script that progressively adds bad panels to the dashboard over simulated time, showing the score degrade on a trend chart. Supports US7.

---

## 8. Product shape decision matrix

| Criterion | External web service | Grafana App Plugin (advisor-app pattern) | CI/CD lint gate |
|---|---|---|---|
| **Time to first value** | 3–4 weeks (fastest) | 6–8 weeks (plugin scaffolding + Check interface) | 2–3 weeks (CLI only) |
| **User adoption friction** | Medium — separate URL, separate auth | **Low** — embedded in Grafana where users work | **Low** — runs automatically |
| **Integration depth** | Shallow — API calls only | **Deep** — Check/Step interfaces, dashboard context, fix-it links, scheduled re-evaluation | None — offline analysis |
| **Required permissions** | Grafana API key (Viewer role) | Plugin install rights; service account (Viewer) | File system access; optional API key |
| **Maintenance burden** | Own deployment, own auth, own UI | Must track Grafana plugin SDK versions; advisor-app Check interface changes | Minimal — stateless CLI |
| **Existing UX framework** | Build from scratch | **`grafana-advisor-app`** provides severity model, scheduling, resolution tracking | N/A |
| **Covers prevention** | No (reactive only) | No (reactive only) | **Yes** — blocks bad dashboards pre-merge |
| **Covers live diagnosis** | Yes | Yes | No |
| **Git Sync compatibility** | Via API | Via plugin + API | **Native** — operates on JSON files in repo |

**Recommended approach**: Build the analysis engine as a **standalone Go library** first (library-first architecture). Expose simultaneously as: (1) CLI for CI/CD (fastest, works with Git Sync), (2) HTTP service for web UI (interactive analysis), (3) Grafana App Plugin backend implementing `checks.Check` interface (embedded experience with advisor-app UX). Same codebase, three delivery mechanisms.

---

## 9. Risks and mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **False positives from static analysis** — flagging low-cardinality metrics as expensive | High | Phase 2 cardinality enrichment via TSDB status API. Present static findings as "potential issues" with confidence indicators. Allow per-rule and per-dashboard suppression via `.lint` config. |
| **Recommendation fatigue** — 40+ findings overwhelm users | High | Limit default view to top 5 highest-impact. Group related findings ("12 panels have missing label filters" as one card). Implement mute/resolve workflow (Datadog pattern). |
| **Grafana version compatibility** — JSON schema evolves | Medium | Use `grafana-foundation-sdk` for schema-accurate parsing (auto-generated from Grafana schemas). Test against JSON from Grafana 10, 11, 12. Version detection + normalization layer. |
| **Pint `internal/` packages** — cannot import as Go library | Medium | Fork repo and lift ~10 files from `internal/checks/promql/*` into our codebase (self-contained, depend only on Prometheus parser). Alternative: wrap `pint lint` as subprocess with synthetic rule YAML. |
| **Mimirtool AGPL license** — viral license risk | Medium | Reimplement the metric-extraction pattern (~200 lines wrapping Prometheus parser). Do not import AGPL code. |
| **Template-variable fuzzy matching** — logged queries differ from dashboard expressions | Medium | Normalization layer: strip `$variable` interpolations, canonicalize whitespace/label order, similarity hashing. Accept partial matches above confidence threshold. |
| **PromQL dialect differences** — Thanos extensions, Mimir hints | Low | Use Prometheus parser (handles standard PromQL). Add post-parse extension layer for Thanos-specific constructs. Log and skip unparseable expressions. |
| **Performance of advisor itself** — analyzing hundreds of dashboards | Low | Cache TSDB status responses (5-min TTL). Async job queue. Static analysis <100ms per dashboard; enriched <2s. |
| **Scoring calibration** — initial weights imprecise | Medium | All weights/thresholds configurable. Collect feedback via mute/resolve. Phase 3 runtime profiling validates and recalibrates static scoring. |
| **Security** — dashboard JSON contains sensitive info | Medium | Viewer-role API keys only (no write). Run within same trust boundary as Grafana. No long-term storage of raw JSON. Per-org API key scoping for multi-tenancy. |

---

## 10. Appendix: references and links

**Grafana documentation and tools**
- Panel Inspector: `grafana.com/docs/grafana/latest/panels-visualizations/panel-inspector/`
- Dashboard JSON model: `grafana.com/docs/grafana/latest/dashboards/build-dashboards/view-dashboard-json-model/`
- Query optimization blog: `grafana.com/blog/grafana-dashboards-tips-for-optimizing-query-performance/`
- App Plugin development: `grafana.com/developers/plugin-tools/tutorials/build-an-app-plugin`
- Internal metrics: `grafana.com/docs/grafana/latest/setup-grafana/set-up-grafana-monitoring/`
- Dashboard HTTP API: `grafana.com/docs/grafana/latest/developers/http_api/dashboard/`
- Advisor plugin page: `grafana.com/grafana/plugins/grafana-advisor-app/`
- Git Sync (Grafana 12): `grafana.com/blog/git-sync-grafana-12/`
- Dashboard-linter CI integration: `github.com/grafana/grafana/pull/97199`
- Cardinality Explorer dashboard (ID 11304): `grafana.com/grafana/dashboards/11304`

**Thanos documentation**
- Query-frontend: `thanos.io/tip/components/query-frontend.md/`
- Store Gateway: `thanos.io/tip/components/store.md/`
- EXPLAIN proposal: `github.com/thanos-io/thanos/issues/5911`
- Granular query metrics proposal: `thanos.io/tip/proposals-accepted/202108-more-granular-query-performance-metrics.md/`

**Prometheus**
- Query log guide: `prometheus.io/docs/guides/query-log/`
- TSDB status API: `prometheus.io/docs/prometheus/latest/querying/api/` (TSDB status section)
- PromQL parser source: `github.com/prometheus/prometheus/promql/parser/`

**Grafana Mimir**
- Cardinality API: `grafana.com/docs/mimir/latest/references/http-api/#cardinality`
- Query-frontend: `grafana.com/docs/mimir/latest/references/architecture/components/query-frontend/`
- CERN cardinality dashboards: `github.com/cerndb/grafana-mimir-cardinality-dashboards/`

**Datadog and competitors**
- DBM Recommendations: `docs.datadoghq.com/database_monitoring/recommendations/`
- Watchdog Insights: `docs.datadoghq.com/watchdog/insights/`
- Metrics Without Limits: `docs.datadoghq.com/metrics/metrics-without-limits/`
- New Relic Compute Optimizer: `docs.newrelic.com/docs/accounts/accounts-billing/new-relic-one-pricing-billing/compute-optimizer/`
- Splunk AI Assistant: `splunk.com/en_us/blog/artificial-intelligence/splunk-ai-assistant-for-spl-1-4.html`

**Open-source repos**
- `github.com/grafana/dashboard-linter` — dashboard JSON lint (312 stars, Apache-2.0)
- `github.com/grafana/grafana-advisor-app` — Grafana advisor plugin UX framework (8 stars, Apache-2.0)
- `github.com/cloudflare/pint` — Prometheus rule linter (1,000 stars, Apache-2.0)
- `github.com/prometheus/promlens` — PromQL tree visualizer (663 stars, Apache-2.0)
- `github.com/grafana/mimir` — mimirtool `analyze grafana` (4,300 stars, AGPL-3.0)
- `github.com/grafana/grafana-foundation-sdk` — typed dashboard SDK (201 stars, Apache-2.0)
- `github.com/grafana-tools/sdk` — Go Grafana SDK (400 stars, Apache-2.0)
- `github.com/VictoriaMetrics/metricsql` — standalone PromQL parser (241 stars, Apache-2.0)
- `github.com/slok/sloth` — SLO recording-rule generator (2,200 stars, Apache-2.0)
- `github.com/chronosphereio/high-cardinality-analyzer` — query log analyzer (50 stars, MIT, archived)
- `github.com/thought-machine/prometheus-cardinality-exporter` — cardinality metrics (43 stars, Apache-2.0)
- `github.com/ContainerSolutions/prom-metrics-check` — dashboard metric validator (66 stars, MIT)
- `github.com/esnet/gdg` — bulk dashboard management (200 stars, BSD-3)
- `github.com/prometheus-community/promql-langserver` — PromQL LSP (180 stars, Apache-2.0)
- Pint documentation: `cloudflare.github.io/pint/`

# Codex Token and Speed Benchmark

This benchmark compares Codex native documentation lookup against DocuMcp-backed lookup.

## Prerequisites

- `codex` CLI installed and logged in.
- DocuMcp running with at least one crawled source.
- If semantic search is part of the claim, run DocuMcp with `DOCUMCP_MODEL_PATH` pointing at the ONNX model directory.

For web sources, prefer the site root plus one or more include paths instead of a deep page URL. For example:

| Source | URL | Include path |
| --- | --- | --- |
| Kubernetes | `https://kubernetes.io/` | `/docs/` |
| React | `https://react.dev/` | `/reference/react/` |
| Go docs | `https://go.dev/` | `/doc/` |

This gives the crawler access to root-level sitemaps and navigation links while still limiting indexed pages to the documentation section you want to benchmark.

## Task File

Tasks are JSONL. Each line is one documentation question:

```json
{"id":"k8s-readiness","prompt":"How do I configure a readiness probe?","source":"Kubernetes","expected_contains":["readinessProbe"],"expected_url_contains":["/readiness"]}
```

Fields:

- `id`: stable task identifier.
- `prompt`: the question Codex answers.
- `source`: optional DocuMcp source name. When set, the DocuMcp prompt tells Codex to pass this source to `search_docs`.
- `expected_contains`: optional answer substrings for lightweight correctness checks.
- `expected_url_contains`: optional URL substrings checked against the final answer and raw Codex JSONL events.

The example task file includes two tiers: three simple single-page lookups followed by multi-piece questions that should require combining related documentation sections. Keep both tiers when evaluating whether DocuMcp helps more as question complexity increases.

## Run

Build the benchmark runner:

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/bench ./cmd/bench
```

Run both modes once per task:

```bash
./bin/bench \
  -tasks docs/bench/codex-tasks.example.jsonl \
  -mode both \
  -runs 1 \
  -documcp-url http://localhost:8080/mcp/http \
  -out bench-results.json \
  -raw-dir bench-events
```

Native mode runs:

```bash
codex --search exec --ignore-user-config --json ...
```

DocuMcp mode runs:

```bash
codex exec --ignore-user-config --json -c 'mcp_servers.documcp.url="http://localhost:8080/mcp/http"' ...
```

`--ignore-user-config` keeps the comparison isolated from MCP servers or other defaults in your normal Codex config. Codex authentication is still read from `CODEX_HOME`.

The runner writes `bench-results.json` with per-run durations, token metrics found in Codex JSONL events, web-search call counts, MCP tool-call counts, final answers, and lightweight correctness hints.
It also writes raw Codex JSONL event logs to `bench-events/` so cancelled tool calls and token accounting can be inspected after a run.

DocuMcp benchmark prompts intentionally steer Codex through the compact path: one `search_docs` call first, answer from snippets when sufficient, and use at most one `get_page_excerpt` call before falling back to full `get_page`.

To measure fixed MCP attachment overhead without retrieval, run:

```bash
make bench-run BENCH_MODE=mcp_noop
```

To run native, DocuMcp retrieval, and the MCP no-op diagnostic in one pass:

```bash
make bench-run BENCH_MODE=all
```

## Interpreting Results

Use medians for presentation:

- `median_total_tokens`: lower is better.
- `median_duration_ms`: lower is faster.
- `median_tool_calls`: lower usually means less lookup churn.
- `success_rate`: must stay comparable; token savings are not meaningful if correctness drops.

If Codex changes its JSONL event schema and token fields are missing, the benchmark still reports wall-clock time, final answers, output size, and tool-call counts. In that case, use an API-controlled Responses benchmark for exact token accounting.

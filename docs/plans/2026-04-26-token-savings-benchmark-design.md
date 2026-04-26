# Token-Savings Benchmark — Design

**Date:** 2026-04-26
**Branch:** `bench/token-savings`
**Status:** Spec — not yet planned or implemented

## 1. Overview & Goal

**Question being answered:** Does using DocuMcp instead of letting an agent freely fetch public documentation actually reduce token consumption — and by how much, on what kinds of tasks?

Two measurements, reported separately:

- **Per-page token diff** (option 1): for the same URL, how many tokens does DocuMcp's extracted text consume vs. a naive HTML→text strip of the raw page? Static, deterministic, no API cost. Sanity check.
- **Task benchmark** (option 2): on a hand-curated set of doc-lookup questions, how many tokens does an agent consume to reach a *correct* answer using DocuMcp's MCP tools vs. using `web_search` + a custom HTTP-fetch tool? Real Claude Sonnet 4.6 in the loop, real cost, real answer.

The publishable claim comes from option 2. Option 1 is an "is the gap even real on a per-page basis" check — useful for debugging the harness and for a single-line summary stat, but not the headline.

### Non-goals

- Not benchmarking answer *quality* beyond binary correct/incorrect
- Not benchmarking latency or wall-clock time
- Not benchmarking Haiku 4.5 or Opus 4.7 (deferred to v2)
- Not auto-generating the question corpus (hand-curated only)
- Not running in CI (one-shot research tool)
- Not testing a "DocuMcp + web_search" hybrid configuration (deferred to v2)

## 2. Architecture & Layout

```
cmd/bench/
  main.go              # CLI entry: bench page-diff | bench tasks | bench all | bench sample-urls
internal/bench/
  pagediff/            # Option 1: per-page token comparison
    pagediff.go        # Iterate URLs, fetch raw, strip, count tokens for each
    strip.go           # HTML→text stripper (option C; reused by tasks/ as a tool impl)
  tasks/               # Option 2: task benchmark
    runner.go          # Question loop, two configurations, agent invocations
    config_a.go        # web_search + fetch_url tool definitions/handlers
    config_b.go        # DocuMcp MCP tool wiring (HTTP→/mcp/* on local server)
    judge.go           # Correctness judge: separate Claude call, returns bool + reason
    types.go           # Question, Run, Result, Report
  corpus/
    questions.json     # Hand-curated question set, committed to repo
    page-urls.txt      # URL list for option 1 (committed; regenerated via sample-urls)
  report/
    json.go            # Emit results as JSON
    markdown.go        # Emit human-readable summary table
  tokens/
    count.go           # Wrapper around Anthropic count_tokens API
docs/superpowers/specs/
  2026-04-26-token-savings-benchmark-design.md   # this file
docs/plans/
  2026-04-26-token-savings-benchmark-impl.md     # implementation plan (next step)
```

### Key design choices

- New top-level `internal/bench/` package, kept separate from production code so `cmd/documcp/` cannot accidentally import it.
- The HTML stripper in `pagediff/strip.go` is shared with `tasks/config_a.go` (it implements the `fetch_url` tool in config A). One implementation, two callers.
- The task benchmark talks to a *running* DocuMcp instance over HTTP via the `/mcp/*` endpoints, not by importing internal packages directly. This guarantees we measure what an actual MCP client would see.
- `corpus/questions.json` and `corpus/page-urls.txt` ship in the repo so results are reproducible; the corpus is version-controlled.
- Build flags follow the rest of the project: `CGO_ENABLED=1 -tags sqlite_fts5`. New `Makefile` target: `make bench`.

## 3. Per-Page Benchmark (option 1)

Goal: for a given list of URLs, report the token count of (a) raw HTML, (b) HTML stripped to text via our minimal stripper, (c) DocuMcp's `get_page` output.

### URL source

Read from `internal/bench/corpus/page-urls.txt`, one URL per line. Defaults to a representative sample of URLs already indexed by the local DocuMcp instance.

A helper subcommand seeds this file:

```
bench sample-urls --per-source 5
```

It queries the running DocuMcp REST API for each source and writes up to N URLs per source. Run once manually and commit the result to keep the corpus stable and reviewable.

### Per-URL procedure

1. `http.GET` with a 30s timeout, 5 MiB body cap (mirrors the `github_repo` adapter convention) — record `tokens_raw`.
2. Run the same body through `internal/bench/pagediff/strip.go` — record `tokens_stripped`.
3. Call DocuMcp's `get_page` (over HTTP, same path the MCP tool takes) — record `tokens_documcp`.
4. Counts come from `tokens/count.go`, which calls Anthropic's `messages/count_tokens` endpoint with the text wrapped as a single user message.

### Stripper rules

The stripper deliberately matches what a *naive* agent without DocuMcp would do — not what `extract.go` does. Less aggressive on purpose, so the per-page win is real, not an artifact of comparing apples to apples.

- Drop `<script>`, `<style>`, `<noscript>`, `<iframe>` element subtrees.
- Collapse whitespace runs to a single space.
- Preserve text in all other elements, including `<nav>`, `<footer>`, `<header>`, `<aside>` — an agent without DocuMcp would not know to skip them.

### Output (per-URL row)

```
url, tokens_raw, tokens_stripped, tokens_documcp,
  ratio_stripped_over_documcp, ratio_raw_over_documcp
```

### Failure handling

If any of the three fetches fails (timeout, non-2xx, body over cap), the URL is logged and skipped — its row is omitted from the report rather than zeroed out. A summary at the bottom shows N succeeded / N skipped.

No retries, no concurrency. Sequential, predictable, easy to debug. ~20 URLs × 3 fetches × ~1s ≈ 1 minute total.

## 4. Task Benchmark (option 2)

Goal: for each curated question, run an agent loop in two configurations (A: no DocuMcp, B: DocuMcp), measure tokens consumed, judge correctness, report.

### Trials

Per question, per configuration: **3 independent trials.**

### Agent loop (identical shape across configs)

1. Send the question to Claude Sonnet 4.6 via the Messages API with `tools` set to that config's tool list and the system prompt below.
2. While the response has `stop_reason == "tool_use"`:
   - Execute each tool call locally, append result as a `tool_result` message.
   - Send the conversation back, accumulating `input_tokens` + `output_tokens` from every API response.
3. When `stop_reason == "end_turn"`, the final assistant text is the answer.
4. Hard limits: max 15 tool-use rounds, max 30s per individual tool call, max 5 minutes wall-clock per trial. Any limit hit → trial marked `aborted`. Tokens are still recorded but excluded from the headline number.

### System prompt (identical across configs)

> You are answering a documentation question. Use the available tools to find the answer. Cite the URL of the page where you found the information. Keep your final answer concise — quote or paraphrase only what's needed to answer.

The "cite the URL" requirement is what lets the judge verify the answer came from a real source rather than the model's training data.

### Configuration A — no DocuMcp

- `web_search`: Anthropic's built-in `web_search_20250305` server tool (handled by Anthropic, no local impl).
- `fetch_url(url: string)`: local function tool. Calls into `pagediff/strip.go`. Returns the stripped body, truncated to 50,000 characters with a `...[truncated]` marker if needed (matches the effective cap config B has via `get_page`).

### Configuration B — DocuMcp

- The four MCP tools (`list_sources`, `search_docs`, `browse_source`, `get_page`) wired through to the locally-running DocuMcp HTTP `/mcp/*` endpoints.
- No `web_search` is provided — testing the pure DocuMcp path.

### Judge

- After each trial, a separate API call to Sonnet 4.6 with `tools=[]`.
- Inputs: the question, the agent's final answer, the cited URL(s), and the actual content of those URLs. For each cited URL: query `GET /api/pages/{url}` on the running DocuMcp; if it returns 200, use that body. Otherwise fall back to raw HTTP GET + the same stripper used in option 1 (capped at 50,000 chars).
- Output: structured JSON `{ "correct": bool, "reason": string }`.
- Judge tokens are tracked separately in the report — they do not count toward either configuration's score.

### Per-trial result row

```
question_id, config (A|B), trial (1|2|3),
  input_tokens, output_tokens, total_tokens,
  tool_calls (count), aborted (bool),
  correct (bool), judge_reason
```

### Headline metric

For each configuration, mean total tokens across *correct, non-aborted* trials, with a 95% percentile bootstrap confidence interval (10,000 resamples).

Also reported per configuration: correctness rate, mean tool-call count, abort rate.

### Cost ballpark

20 questions × 2 configs × 3 trials = 120 agent runs. At ~30k tokens average per run (conservative), ~3.6M tokens total. At Sonnet 4.6 prices: a few dollars per full run. Plus 120 cheap judge calls. Affordable for a research run, fully repeatable.

## 5. Question Corpus Format

**File:** `internal/bench/corpus/questions.json` — version-controlled, hand-curated.

### Per-entry schema

```json
{
  "id": "fts5-trigram-tokenizer",
  "tier": 1,
  "question": "What's the exact `tokenize=` value for FTS5's trigram tokenizer with a minimum length of 3?",
  "expected_source": "sqlite-docs",
  "expected_url_pattern": "sqlite\\.org/fts5\\.html",
  "reference_excerpt": "tokenize = 'trigram detail=column case_sensitive 0' — see §4.3.5 trigram tokenizer; tokendata=1 unsupported",
  "notes": "Tier 1 single-fact lookup. Answer is in one section of one page."
}
```

### Field semantics

- `id`: stable kebab-case identifier, used for filename-safe per-question logging.
- `tier`: 1 / 2 / 3 (single-fact / config-syntax / multi-page synthesis). Used for tier-stratified reporting.
- `question`: exact text sent to the agent. No system-prompt overrides.
- `expected_source`: must match a `name` in the running DocuMcp instance. Validated at startup so the harness cannot silently test unindexed material.
- `expected_url_pattern`: regex. A passing answer must cite at least one URL that matches it — guards against agents bluffing answers without a real cite.
- `reference_excerpt`: the ground-truth snippet, given to the judge as the "correct answer looks like this" anchor. Does not need to be verbatim from the page — a paraphrase is fine.
- `notes`: free-text scratch space; ignored by the harness.

### Startup validation

- All `expected_source` names exist in the running DocuMcp instance (queried via `GET /api/sources`).
- Every `expected_url_pattern` regex compiles.
- `tier` ∈ {1, 2, 3}.
- `id` is unique across the file.
- Any failure → harness exits with a clear error before any API calls are made.

### Initial corpus shape

15 questions, ~9 tier-1 / ~5 tier-2 / ~1 tier-3 (60/30/10 split). The repo ships with one example entry to anchor the format; the rest are hand-written by the operator.

### Example questions (templates for the operator to rephrase)

These are illustrative — they assume specific docs are indexed. The operator picks ones that match their own DocuMcp corpus and rephrases them in their own voice.

*Tier 1 — single-page, single fact*
- "What's the default value of SQLite's `synchronous` PRAGMA when WAL mode is enabled?"
- "What HTTP status code does GitHub's REST API return when a fine-grained PAT lacks the required permission for a repo endpoint?"

*Tier 2 — config / syntax recall*
- "Show me the exact `tokenize=` value for FTS5's trigram tokenizer with a minimum length of 3."
- "Write a `go.mod` `replace` directive that points `example.com/foo` at a local relative path."

*Tier 3 — multi-page synthesis*
- "When should I use `context.WithCancel` vs. `context.WithTimeout` vs. `context.WithDeadline`, and what's the parent-cancellation behavior of each?"
- "If I add a `NOT NULL` column with a default to a large Postgres table, what's the locking behavior on PG 11+ vs. PG 10, and what's the safe migration pattern?"

## 6. Reporting

Two output files per run, written to `bench-results/<timestamp>/`:

### `results.json`

Full machine-readable record:
- Every per-URL row from option 1.
- Every per-trial row from option 2.
- Run metadata: model, git SHA, DocuMcp version, corpus hash, timestamp.

Stable schema, suitable for diffing across runs.

### `summary.md`

Human-readable markdown for sharing. Includes:

- **Headline table:** Config A vs Config B — mean total tokens (correct trials), savings %, 95% CI.
- **Per-tier breakdown:** same numbers stratified by tier 1/2/3.
- **Per-page table** (option 1): top 10 URLs by `ratio_stripped_over_documcp`, plus mean across all URLs.
- **Correctness & abort rates** per config.
- **Judge token cost** on a separate line so it cannot be confused with agent costs.
- **Aborted / skipped log:** any URLs that failed in option 1, any trials that hit a hard limit in option 2.

### Stability across runs

The corpus is fixed and version-controlled, so re-running the benchmark on a different DocuMcp version (or after tweaking `extract.go`) produces directly comparable numbers.

## 7. CLI Surface

```
bench page-diff [--urls FILE]                # option 1 only
bench tasks     [--questions FILE] [--trials N]  # option 2 only
bench all                                    # both, single output dir
bench sample-urls --per-source N             # helper to seed page-urls.txt
```

Defaults:
- `--urls` → `internal/bench/corpus/page-urls.txt`
- `--questions` → `internal/bench/corpus/questions.json`
- `--trials` → 3
- DocuMcp instance URL: `http://127.0.0.1:8080` (override via `DOCUMCP_BENCH_URL` env var)
- Anthropic API key: read from `ANTHROPIC_API_KEY` env var

## 8. Open Items

None blocking implementation. Operator must:

1. Curate ~15 questions in `corpus/questions.json` before the first task-benchmark run.
2. Run `bench sample-urls` once to seed `corpus/page-urls.txt` and commit the result.
3. Set `ANTHROPIC_API_KEY` and ensure the local DocuMcp instance is running before invoking `bench tasks` or `bench all`.

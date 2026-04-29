# Benchmark — Methodology and Runbook

This document explains what the DocuMcp benchmark measures, how it measures it, and how to run it end-to-end. For the original design rationale see `docs/plans/2026-04-26-token-savings-benchmark-design.md`. For implementation details see `docs/plans/2026-04-26-token-savings-benchmark-impl.md`.

---

## 1. What the benchmark measures

**The question:** does an AI agent that uses DocuMcp's MCP tools consume fewer tokens than the same agent without DocuMcp, when both are answering documentation lookup questions?

**The hypothesis:** yes, because:
1. DocuMcp returns extracted, denoised text (no nav, scripts, footers, ads) — fewer tokens per page than a raw HTTP fetch.
2. DocuMcp's `search_docs` returns ranked snippets in one call, so the agent doesn't have to web-search → fetch candidate → realize it's wrong → fetch another → etc.

**What "fewer tokens" means concretely:** the input + output tokens reported by Anthropic's Messages API across an entire agent loop, until the agent produces a final answer.

The benchmark separates this into two measurements that are reported independently.

### 1.1 Per-page token diff (option 1)

For a fixed list of URLs, compare the token count of:

- (a) the **raw HTML** an `http.Get` returns
- (b) that HTML **stripped to plain text** via a deliberately naive walker (drops only `<script>`, `<style>`, `<noscript>`, `<iframe>`; keeps nav/footer/header/aside)
- (c) the text **DocuMcp's `get_page` returns** for the same URL

Token counts come from Anthropic's free `/v1/messages/count_tokens` endpoint.

The naive stripper deliberately approximates what an agent without DocuMcp would have if asked to dump a page's text — less aggressive than DocuMcp's own `internal/db/extract.go`. This keeps the comparison honest.

**What this measurement tells you:** for any given URL, how many tokens does DocuMcp's pre-processing save on a single fetch. Useful for sanity-checking the order of magnitude of the gap and for diagnosing per-source quality. Sequential, deterministic, near-free to run.

**What it does NOT tell you:** whether an agent solving a real task ends up needing fewer fetches. That's option 2.

### 1.2 Task benchmark (option 2)

For each question in a hand-curated corpus, run an agent in two configurations and measure end-to-end token consumption to a correct answer.

**Configuration A — no DocuMcp (the baseline):**
- `web_search` (Anthropic's built-in `web_search_20250305` server tool) — for URL discovery
- `fetch_url(url)` — custom function tool that does HTTP GET + the same naive stripper from option 1, with a 50,000-character cap matching what option B's `get_page` effectively returns

**Configuration B — with DocuMcp:**
- The four DocuMcp MCP tools: `list_sources`, `search_docs`, `browse_source`, `get_page`
- Talked to over the SSE-backed MCP transport at `/mcp/`

Both configurations get the same question, the same system prompt, and the same model (Claude Sonnet 4.6). The only variable is the toolset.

**Per-trial procedure:**
1. Send the question to the model with the configuration's tool list.
2. While the response is `tool_use`: execute each tool call locally, append the assistant's tool_use blocks and the matching tool_result blocks, send the conversation back. Accumulate input + output tokens from every API response.
3. When the response is `end_turn`: capture the final answer text.
4. Hard limits: 15 tool-use rounds max, 30s per individual tool call, 5 minutes wall-clock per trial. Hitting any limit marks the trial `aborted` and excludes it from the headline.

**Citation tracking:** after each trial, the harness records `CitedURLs` from two sources combined and deduped:
- The URL arguments passed to `fetch_url` and `get_page` calls (URLs the agent actually fetched)
- URLs found via regex on the answer text (catches `web_search`-mediated cites)

**Trials per question:** 3 by default (configurable via `--trials N`).

**Correctness scoring (the judge):** after each trial, a separate Claude Sonnet 4.6 call (no tools) is given the question, the agent's final answer, the corpus's `reference_excerpt`, and the actual content of the cited URLs. It returns a JSON `{correct: bool, reason: string}` verdict.

**Anti-hallucination guards (run before the judge to save tokens):**
- If the agent cited zero URLs → auto-fail. Reason: `"no URL cited"`.
- If no cited URL matches the question's `expected_url_pattern` → auto-fail. Reason: `"cited URL did not match expected_url_pattern"`.
- Otherwise: run the judge, take its verdict and reason.

**The headline metric:** for each configuration, the mean total tokens (input + output) across **correct, non-aborted** trials, with a 95% percentile bootstrap confidence interval (10,000 resamples, deterministic seed).

Judge tokens are tracked separately and never enter the per-config totals.

---

## 2. What we are trying to achieve

The publishable claim has the form:

> "On a corpus of N hand-curated documentation lookup questions, an agent using DocuMcp consumed X% fewer tokens (95% CI: [Y%, Z%]) than the same agent using `web_search` + raw HTTP fetching, with comparable correctness rates."

Three things this claim deliberately does NOT cover:

- **Answer quality beyond binary correct/incorrect.** Two correct answers with very different fluency or depth count the same.
- **Latency or wall-clock time.** Tokens, not seconds.
- **Out-of-corpus questions.** The corpus is curated to questions whose answers exist in the operator's indexed sources. The benchmark says nothing about questions DocuMcp has not indexed.

If the result is "DocuMcp did NOT save tokens," that's also a valid output — it tells the operator their source mix isn't pulling its weight, or that the questions they care about are easier to answer via web search than via their indexed corpus.

---

## 3. Prerequisites

Before running anything:

1. **A running DocuMcp instance** with at least one source indexed.
   - Default URL: `http://127.0.0.1:8080`. Override with `DOCUMCP_BENCH_URL=http://...`.
   - If the instance requires a bearer token, set `DOCUMCP_API_KEY=...` — the bench passes it as `Authorization: Bearer ...` on both REST and MCP requests.

2. **An Anthropic API key.** `export ANTHROPIC_API_KEY=sk-ant-...`.
   - Required for both subcommands: option 1 calls `count_tokens` (free but auth'd); option 2 calls Messages (paid).

3. **The bench binary built.** Run `make bench` from the repo root → produces `bin/bench`.

The benchmark assumes you control the DocuMcp instance you're pointing at. It does not work against a remote/shared instance unless you also set `DOCUMCP_API_KEY` and the instance accepts your token.

---

## 4. Choosing what to run

| Subcommand | What it does | Cost | When to run |
|---|---|---|---|
| `bench page-diff` | Option 1 only: per-page token diff for ~20 URLs | ~free (only `count_tokens` calls) | First — confirms DocuMcp's pre-processing actually saves tokens per URL. Quick smoke test of the wiring (pulls URLs, hits MCP, talks to Anthropic). |
| `bench tasks` | Option 2 only: full agent benchmark | ~few USD per run | Second — the real number. Run after `page-diff` confirms the gap is real and the harness works. |
| `bench all` | Both, into one output directory | Same as tasks | Convenience for a one-shot full run. ⚠️ Currently overwrites `results.json` (page-diff one is lost; tasks wins). Markdown is fine. Workaround: run separately. |
| `bench sample-urls --per-source N` | Helper. Walks the running DocuMcp's sources via MCP `browse_source` and writes `internal/bench/corpus/page-urls.txt` | Free (no Anthropic calls) | Once, before `page-diff`. |

---

## 5. Step-by-step: running the benchmark

### 5.1 Set environment variables

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export DOCUMCP_BENCH_URL=http://127.0.0.1:8080   # only if non-default
export DOCUMCP_API_KEY=...                       # only if your DocuMcp requires it
```

### 5.2 Build the binary

```bash
make bench
```

Produces `bin/bench`. Re-run after any code change.

### 5.3 Seed the URL list (option 1 path)

```bash
./bin/bench sample-urls --per-source 5
```

Walks each source via MCP `browse_source` and writes up to 5 URLs per source into `internal/bench/corpus/page-urls.txt`. Inspect the file before running `page-diff` — the heuristic that picks URLs is intentionally simple and may need cleanup, especially for sources whose `browse_source` output doesn't follow the expected text format.

You can also write `page-urls.txt` by hand: one URL per line, lines starting with `#` are ignored. Commit the file if you want reproducible runs.

### 5.4 Run the per-page diff

```bash
./bin/bench page-diff
```

Per-URL output streams to stdout (success/skip log). Final output written to `bench-results/<UTC-timestamp>/{results.json,summary.md}`.

If many URLs are being skipped, check the error log in `summary.md` — typical causes are timeouts, 4xx/5xx, body-size cap (5 MiB), or `get_page` returning empty for URLs not actually indexed.

### 5.5 Curate the question corpus (option 2 path)

The repo ships `internal/bench/corpus/questions.json` with one example entry showing the shape. **You must replace it** with hand-curated questions before running `bench tasks`. The harness validates `expected_source` against `GET /api/sources` at startup, so unknown sources are caught before any API spend.

A good question has:

- **A specific factual answer** that lives in a single section of a single page (Tier 1) or requires synthesizing across a few pages (Tier 2/3).
- **Realistic phrasing** — what someone would actually type, not a doc heading reversed into a question.
- **A specific anchor** the answer should cite: a flag name, a default value, an exact configuration string, a version number. Avoid questions Claude can answer from training memory alone.
- **An indexed source.** If the answer is not in your indexed corpus, DocuMcp can't win — and Configuration A might "win" by web-searching the live docs. The benchmark is testing whether DocuMcp helps when the answer is in its index.

Per-entry schema:

```json
{
  "id": "kebab-case-id",
  "tier": 1,
  "question": "What's the exact `tokenize=` value for FTS5's trigram tokenizer?",
  "expected_source": "sqlite-docs",
  "expected_url_pattern": "sqlite\\.org/fts5\\.html",
  "reference_excerpt": "tokenize = 'trigram detail=column case_sensitive 0' — see §4.3.5",
  "notes": "Tier 1 single-fact lookup."
}
```

Tier hints:
- **Tier 1** — single page, single fact. Where DocuMcp wins biggest (one search → done).
- **Tier 2** — config or syntax recall, may require following a link.
- **Tier 3** — multi-page synthesis, comparison, edge-case behavior. Most realistic of common LLM coding tasks.

A balanced corpus is 60% Tier 1 / 30% Tier 2 / 10% Tier 3 (~15 questions total: 9/5/1). Smaller corpora work but the per-tier breakdown becomes statistically noisy.

### 5.6 Run the task benchmark

```bash
./bin/bench tasks
```

For each question × {Config A, Config B} × 3 trials = 6 runs per question. With 15 questions, that's 90 trials plus 90 judge calls. Per-trial progress streams to stdout:

```
fts5-trigram-tokenizer [A/1] tokens=18723 correct=true aborted=false
fts5-trigram-tokenizer [A/2] tokens=21102 correct=true aborted=false
fts5-trigram-tokenizer [A/3] tokens=19880 correct=false aborted=false
fts5-trigram-tokenizer [B/1] tokens=4421 correct=true aborted=false
...
```

Final report → `bench-results/<UTC-timestamp>/{results.json,summary.md}`.

To reduce cost, override the trials count: `./bin/bench tasks --trials 1`. With 1 trial per (question, config), the bootstrap CI is meaningless but you get a fast cost-estimation run.

### 5.7 Read the output

`bench-results/<timestamp>/summary.md` is the human-readable report. It contains:

- **Headline:** Config A vs Config B mean tokens (correct, non-aborted trials only) with 95% CI and savings percentage. This is the publishable number.
- **Per-tier breakdown:** the same numbers stratified by Tier 1 / 2 / 3. Often Tier 1 shows the biggest DocuMcp win and Tier 3 the smallest.
- **Per-page top-10:** the URLs where DocuMcp's text is most compressed relative to the naive stripped HTML.
- **Correctness & aborts:** correct rate per config, mean tool-call count, abort rate. If correctness diverges significantly between configs, the headline mean is biased — check the breakdown.
- **Judge token cost:** separate line, never folded into the per-config totals.
- **Skip / abort log:** any URLs (option 1) or trials (option 2) that didn't complete.

`results.json` is the same data in machine-readable form — stable schema, suitable for diffing against future runs (e.g. after a DocuMcp `extract.go` change).

---

## 6. Interpreting results

### 6.1 What the headline number means

"DocuMcp savings: 78%" with CI [62%, 89%] means: across the corpus, in the trials where both configs produced a correct answer, Config B (DocuMcp) used on average 78% fewer tokens than Config A (no DocuMcp), and we're 95% confident the true mean savings is between 62% and 89% given the bootstrap distribution. This is the publishable claim.

A small N (15 questions × 3 trials = 45 trials per config when fully populated, less if some trials abort or get filtered as incorrect) means the CI will be wide. If the lower bound of the CI is below 0%, the savings are not statistically distinguishable from zero on this corpus and you should expand the corpus before claiming a win.

### 6.2 What to look at if the result surprises you

- **Correctness rate divergence.** If Config A is 60% correct and Config B is 95% correct, the headline mean compares only the trials where each config got the answer right. That's the right thing to do (token counts on wrong answers are meaningless), but it can hide a story: maybe Config A only "wins" because it gives up quickly on hard questions, while Config B grinds through them. Check the abort rate too.
- **Aborts.** A high abort rate for Config A on Tier 3 questions suggests the agent without DocuMcp is hitting the 15-round limit because it's web-searching → fetching → realizing it's wrong → re-searching. A high abort rate for Config B suggests `search_docs` isn't returning the right pages — a DocuMcp quality issue.
- **Per-tier breakdown.** Tier 1 dominated by DocuMcp is the expected case. Tier 3 close to zero gap means multi-page synthesis benefits less from pre-search than from raw page access — a useful finding.
- **Per-page top-10.** URLs with very high `stripped/documcp` ratios indicate sources where DocuMcp's `extract.go` is doing real work. URLs with low ratios indicate sources where the pages are already clean (e.g. plain markdown rendering). Targets for improving DocuMcp: the URLs in the middle of the distribution.

### 6.3 What to NOT conclude

- "DocuMcp is X% cheaper than web search" — the benchmark uses `web_search` + a function `fetch_url`, not a raw web fetch loop. Real-world without-DocuMcp behavior varies widely depending on what tools the agent has.
- "DocuMcp will save you X dollars on [specific use case]" — extrapolating from a 15-question corpus to a developer's actual workflow is not justified by this benchmark alone. The benchmark answers a narrower question.
- "DocuMcp scales" — the benchmark measures one model (Sonnet 4.6) on one corpus. Cheaper models (Haiku) may show even bigger savings (they over-fetch more); larger context windows may shrink the gap. Both deferred to v2.

---

## 7. Cost expectations

A full `bench tasks` run at default settings:

- 15 questions × 2 configs × 3 trials = 90 agent runs
- Average ~30k tokens per run (conservative for Sonnet on doc lookup)
- ≈ 2.7M agent tokens
- 90 judge calls, ~5k tokens each ≈ 0.45M judge tokens

At Sonnet 4.6 list prices, this is in the low-single-digit USD range per full run. Re-runs are cheap; the corpus is fixed.

Option 1 (`page-diff`) calls only `count_tokens`, which is free.

---

## 8. Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `bench: required env var ANTHROPIC_API_KEY not set` | Env not exported to current shell | `export ANTHROPIC_API_KEY=...` |
| `corpus: <id>: expected_source "X" not found` | Question's source name doesn't match any name in `GET /api/sources` | Check exact source name in DocuMcp Web UI; update `questions.json` |
| All trials abort on Config B | DocuMcp isn't reachable, or `/mcp/` endpoint returns errors | Curl `http://127.0.0.1:8080/api/sources` to confirm server reachable; check `DOCUMCP_API_KEY` if auth enabled |
| `mcp tool "X" error: ...` | Tool call failed server-side (e.g. bad source name in `search_docs`) | Look at the error reason in `summary.md`'s skip log |
| Many trials fail with `cited URL did not match expected_url_pattern` | Regex too strict, or model is genuinely citing the wrong page | Loosen the regex (e.g. `example\.com` instead of `example\.com/specific/path`) |
| Bootstrap CI is `[NaN, NaN]` for Config A | Zero correct trials in that config | Check correctness rate; the questions may need rephrasing or the corpus may be too hard for Config A |
| `summary.md` looks empty / missing sections | One of the writers failed silently — check `results.json` for the raw data | Re-run; if reproducible, check `bench-results/<ts>/` perms |

---

## 9. Reproducibility

Each run records:
- Git SHA (where the bench code came from)
- SHA-256 hash of the corpus file
- Timestamp (UTC)
- Model name

These are written to both `results.json` and `summary.md`. Two runs with the same git SHA, corpus hash, and DocuMcp instance can be diffed directly. The bootstrap is seeded (seed=1, 10,000 resamples), so CI bounds are stable across runs of the same data.

What's NOT deterministic between runs:
- The model's outputs (no temperature pinning currently — Anthropic Sonnet defaults apply)
- The exact pages `web_search` returns (Config A only)
- Network timing (affects which URLs hit the 30s tool-call limit)

For more deterministic runs, set `--trials 1` and accept that the headline metric becomes a single point estimate rather than a CI'd mean.

---

## 10. Quick reference

```bash
# One-time setup
export ANTHROPIC_API_KEY=sk-ant-...
make bench
./bin/bench sample-urls --per-source 5
$EDITOR internal/bench/corpus/questions.json   # curate ~15 questions

# Cheap sanity check (free)
./bin/bench page-diff

# The real benchmark (~few USD)
./bin/bench tasks

# Inspect
ls bench-results/
$PAGER bench-results/<timestamp>/summary.md
```

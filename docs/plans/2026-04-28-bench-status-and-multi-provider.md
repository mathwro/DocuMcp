# Token-Savings Benchmark — Status & Multi-Provider Notes

**Branch:** `bench/token-savings`
**Status:** Implementation complete (16 feature + 4 fix commits, 34 tests passing). Currently Anthropic-only; this doc captures what's there and what would be needed to support another provider.

## 1. What We Built

### 1.1 Goal

Measure whether using DocuMcp's MCP tools reduces an AI agent's token consumption versus a naive baseline of `web_search` + raw HTTP fetching. Two separate measurements:

- **Per-page diff (option 1):** for each indexed URL, compare token counts of (a) raw HTML, (b) HTML stripped via a deliberately naive walker, (c) DocuMcp's `get_page` output. Static, deterministic, near-free.
- **Task benchmark (option 2):** on hand-curated doc-lookup questions, run an agent tool-use loop in two configurations (A: `web_search` + custom `fetch_url`; B: DocuMcp's four MCP tools) for 3 trials each, with LLM-judged correctness. The publishable claim.

### 1.2 Architecture

Single new binary at `cmd/bench/`. Internal library at `internal/bench/`, fully isolated from production code (it talks to a running DocuMcp instance over HTTP, never imports its internal packages).

```
cmd/bench/
  main.go                   # CLI dispatch: page-diff | tasks | all | sample-urls
  pagediff_cmd.go           # runPageDiff entry + helpers (mustEnv, envOr, fatal, newOutputDir, loadURLs)
  sample_cmd.go             # runSampleURLs + DocuMcp /api/sources fetch
  tasks_cmd.go              # runTasks + per-trial runOneTrial, judgeOne, fetchCitedContent, gitSHA, hashFile
  all_cmd.go                # runAll (parses one combined flag set, calls both Into helpers)

internal/bench/
  tokens/
    count.go                # Anthropic /v1/messages/count_tokens HTTP wrapper
    count_test.go           (2 tests)
  pagediff/
    strip.go                # Naive HTML→text via golang.org/x/net/html
    pagediff.go             # Per-URL runner, injected fetcher/counter
    strip_test.go           (3 tests)
    pagediff_test.go        (2 tests)
  tasks/
    types.go                # Question, TrialResult, JudgeAccounting
    corpus.go               # LoadCorpus with regex compile + source validation
    config_a.go             # FetchURL handler + ConfigATools (web_search + fetch_url)
    config_b.go             # MCP SDK client over SSE + ConfigBTools (4 MCP tools)
    runner.go               # Agent tool-use loop with hard limits (15 rounds, 30s/call, 5min)
    api.go                  # AnthropicAPI implementing the API interface
    judge.go                # Separate Claude call to grade correctness
    corpus_test.go          (5 tests)
    config_a_test.go        (4 tests)
    config_b_test.go        (3 tests, including bearer auth)
    runner_test.go          (5 tests)
    api_test.go             (2 tests)
    judge_test.go           (3 tests)
  report/
    stats.go                # 95% percentile bootstrap CI (10k resamples, seeded)
    json.go                 # Report struct + WriteJSON
    markdown.go             # WriteMarkdown — headline, per-tier, per-page, rates, judge cost, skip log
    stats_test.go           (3 tests)
    report_test.go          (2 tests)
  corpus/
    questions.json          # One example entry; operator replaces with hand-curated set
    page-urls.txt           # Empty placeholder; populated by `bench sample-urls`
```

Total: ~2,640 lines of Go (including tests). 34 unit tests across four packages, all passing. Build wired via `make bench`. Output flags: `bench-results/` and `bin/bench` in `.gitignore`.

### 1.3 Capabilities today

**Per-page diff:**
- HTTP GET each URL (5 MiB cap, 30s timeout, `User-Agent: DocuMcp-Bench/1.0`)
- Token-count raw HTML, stripped HTML, and DocuMcp's `get_page` output via Anthropic's free `count_tokens` endpoint
- Compute `ratio_stripped_over_documcp` and `ratio_raw_over_documcp` per URL
- Skip and log URLs that fail any step (no zero-padding)
- Sequential, no concurrency, ~1 minute for ~20 URLs

**Task benchmark:**
- Loads hand-curated `questions.json`, validates `expected_source` against the running DocuMcp's `/api/sources` (fail-fast at startup)
- Two tool configurations: A = `web_search_20250305` (Anthropic server tool) + custom `fetch_url` (HTTP GET + naive strip, 50k char cap); B = the four DocuMcp MCP tools via SSE-backed SDK client
- Hard limits: 15 tool-use rounds, 30s per individual tool call, 5 min wall-clock per trial. Aborts logged, tokens counted, excluded from headline
- 3 trials per (question, config) by default (`--trials N` to override)
- Per-trial citation tracking: combines URLs from `fetch_url`/`get_page` tool args **and** answer-text regex matches, deduped + sorted
- Judge: separate Sonnet 4.6 call with `tools=[]`, parses `{"correct": bool, "reason": "..."}` from the response (tolerant of prose-wrapped JSON)
- Anti-hallucination guard: trials with no cited URLs OR no URL matching `expected_url_pattern` auto-fail without spending judge tokens

**Reporting:**
- `bench-results/<UTC-timestamp>/results.json` — full machine-readable record (rows, trials, tier map, judge accounting, run metadata: model, git SHA, corpus hash, timestamp)
- `bench-results/<UTC-timestamp>/summary.md` — human-readable: headline table (Config A vs B mean tokens, 95% bootstrap CI, savings %), per-tier breakdown, per-page top-10, correctness/abort rates, judge cost line, skip/abort logs

**CLI:**
```
bench page-diff   [--urls FILE]
bench tasks       [--questions FILE] [--trials N]
bench all         [--urls FILE] [--questions FILE] [--trials N]
bench sample-urls [--per-source N] [--out FILE]
```

**Env vars:**
- `ANTHROPIC_API_KEY` (required for page-diff and tasks)
- `DOCUMCP_BENCH_URL` (default `http://127.0.0.1:8080`)
- `DOCUMCP_API_KEY` (optional bearer)

### 1.4 Known limitations (deferred from final code review)

These were flagged by the whole-branch reviewer; none block running the tool but worth knowing:

- **README claims "no API key required for the diff"** — actually needed (`count_tokens` is free but auth'd). Doc cleanup.
- **`bench all` writes `results.json` for both runs into the same directory**, so the second overwrites the first. The markdown summary is fine. Workaround: run `page-diff` and `tasks` separately into separate output dirs.
- **Bootstrap CIs degenerate at small N** (per-tier with N=3 trials × 1 tier-3 question = 3 samples). Reported as if meaningful. Add a footnote in markdown when N<10.
- **`extractSectionNames` heuristic in `sample-urls`** parses MCP `browse_source` text output via line-length and URL-prefix heuristics. Will likely need adjustment when first run against real data.
- **No per-API-call timeout** in the agent runner — only per-tool-call (30s) and overall (5 min). A hung Anthropic call lets the whole 5-min budget elapse.
- **Stale comment** in `runner.go:11` says `// Production: anthropicAPI{}` (lowercase); actual type is `AnthropicAPI`.

---

## 2. Adopting Another AI Provider

The bench currently couples to Anthropic in three places. Supporting another provider means abstracting each one and supplying provider-specific implementations.

### 2.1 What's Anthropic-specific

| Component | File | Anthropic-specific because |
|---|---|---|
| Token counter | `internal/bench/tokens/count.go` | Posts to `/v1/messages/count_tokens` with `x-api-key` header and `anthropic-version`. Returns `input_tokens` from Anthropic's tokenizer. |
| Agent loop transport | `internal/bench/tasks/api.go` | Posts to `/v1/messages` with `system`/`messages`/`tools` shape; parses `stop_reason`, `usage.input_tokens/output_tokens`, and `content` blocks (`text`, `tool_use` types). |
| Tool-use protocol | `internal/bench/tasks/runner.go` | Constructs assistant messages with `{type: "tool_use", id, name, input}` blocks and user messages with `{type: "tool_result", tool_use_id, content, is_error}` blocks. This shape is **specific to Anthropic** — OpenAI and Gemini have similar but incompatible structures. |
| Built-in web search | `internal/bench/tasks/config_a.go` (`ConfigATools`) | Returns `{"type": "web_search_20250305", "name": "web_search"}` — Anthropic's server-side web search tool. Other providers don't have a drop-in equivalent. |

The MCP client (`config_b.go`) and the per-page diff (`pagediff/`) are **provider-agnostic** — they only need a token counter and a message-loop runner.

### 2.2 Refactor needed

A cross-provider bench needs three abstractions extracted behind interfaces:

#### 2.2.1 `tokens.Counter` (already an interface in spirit)

Currently `*tokens.Counter` is a concrete struct calling Anthropic. Need to:
- Convert to `tokens.Counter` interface with `Count(ctx, text string) (int, error)`
- Rename current implementation `tokens.AnthropicCounter`
- Add per-provider implementations:
  - `tokens.OpenAICounter` — use `tiktoken-go` library (`github.com/pkoukk/tiktoken-go`) with the right encoding for the target model (`o200k_base` for GPT-4/5)
  - `tokens.GeminiCounter` — use Google's `models/gemini-*:countTokens` REST endpoint
  - `tokens.LocalCounter` — for local models, fall back to character-count or load a HuggingFace-compatible tokenizer

**Tradeoff:** Anthropic's `count_tokens` is exact and free. Third-party tokenizers (tiktoken) are exact for OpenAI but not Anthropic. For local models you may have to settle for approximation.

#### 2.2.2 `tasks.API` (already an interface, but `apiResponse` is provider-leaky)

The runner already takes an `API` interface. The leak is in the `apiResponse` struct — its `StopReason` strings (`"end_turn"`, `"tool_use"`) and the `tool_use`/`tool_result` shapes are Anthropic-specific. Need to:
- Define a provider-neutral `tasks.AgentResponse` shape with normalized fields: `Done bool`, `ToolCalls []ToolCall`, `Text string`, `InputTokens int`, `OutputTokens int`
- Each provider's API impl converts its native response into `AgentResponse`
- The runner builds messages via a `tasks.MessageBuilder` interface so each provider can construct its own assistant/user message shape (Anthropic content blocks vs OpenAI `tool_calls` array vs Gemini `parts`)

**Concretely** the work splits into:
- `tasks.AnthropicAPI` (existing) → keep as-is, just makes its own builder
- `tasks.OpenAIAPI` (new) → calls `https://api.openai.com/v1/chat/completions`, parses `choices[0].message.tool_calls[]` and `choices[0].finish_reason`. Different message shape: assistant messages have `tool_calls`, tool responses go in messages with `role: "tool"` and `tool_call_id`.
- `tasks.GeminiAPI` (new) → calls `generativelanguage.googleapis.com/v1beta/models/.../generateContent`. Different again — tool calls are `functionCall` parts inside `content.parts`, responses are `functionResponse` parts.

#### 2.2.3 Web search tool

`web_search_20250305` is Anthropic-specific. For Config A on other providers, swap it for one of:
- **Function tool calling Brave / Tavily / Exa / SerpAPI** — works for any provider, requires another API key, ~$0.005/search
- **Google Search grounding** (Gemini-only built-in, similar to Anthropic's web_search) — flips Gemini's request payload to include a grounding tool
- **No search** — give the agent only `fetch_url` and rely on training-data URL guessing. Honest about what an agent without DocuMcp would actually do, but unfair vs. the Anthropic baseline that does have web_search.

For an apples-to-apples comparison **across providers**, the cleanest approach is "function tool calling Tavily" for everyone, including the Anthropic config. That removes the variable of "Anthropic has built-in search, others don't" from the cross-provider results.

### 2.3 Per-provider effort estimate

| Provider | Complexity | New code | Notes |
|---|---|---|---|
| **OpenAI (GPT-4/5)** | Low | ~250 lines (api.go + tokens) | Best-documented tools API. tiktoken-go is mature. Most straightforward. |
| **Google Gemini** | Medium | ~350 lines | Different message structure (`parts` array with `functionCall`/`functionResponse`). REST `countTokens` available but rate-limited. |
| **xAI Grok** | Medium | ~300 lines | OpenAI-compatible API surface in most regards. Reuse OpenAI adapter with different base URL + model name. |
| **Local (Ollama / llama.cpp)** | High | 500+ lines | Tool-use support varies wildly between models. Token counting needs the model's actual tokenizer. No standard web search. Most realistic for small experiments, not publishable benchmarks. |

### 2.4 Recommended path if you want to do this

Two options depending on your goal:

**Option X — "Run the bench on Provider Y instead of Anthropic" (cheapest path):**
- Pick one provider you have credit for (most likely OpenAI given API maturity).
- Refactor `tokens.Counter` and `tasks.API` into interfaces; add OpenAI implementations.
- Replace `web_search` in Config A with a function tool calling Tavily (or skip search, document the asymmetry).
- Use `tiktoken-go` for counting.
- Rough effort: 1-2 days of focused work. ~400 lines of new Go.

**Option Y — "Compare DocuMcp's effect across multiple providers" (the publishable cross-provider story):**
- All of Option X, plus Gemini and one more (Grok or similar).
- Use Tavily everywhere for fair comparison.
- Add a `--provider` CLI flag and emit per-provider rows in the report.
- Rough effort: 1 week. The bench's design (config A / B / 3 trials / judge) generalizes well; what changes is the API client.

### 2.5 What does NOT need to change

- The MCP client (`config_b.go`) — unaffected by the agent provider; DocuMcp speaks MCP regardless of who's calling it.
- The per-page diff (`pagediff/`) — only needs a token counter swap. Once `tokens.Counter` is an interface, this works for any provider.
- The corpus format (`questions.json`) — provider-neutral by design.
- The reporting code (`report/`) — Provider-neutral; just needs the headline table to optionally include a provider column.
- The judge — any provider can judge; the prompt is plain text and the parser is regex-based. Could even use a different (cheaper) provider for the judge than for the agent loop.

### 2.6 Quickest path to get unblocked right now

If the goal is just "I want to see this benchmark produce numbers without spending Anthropic credit", these are the ordered steps:

1. **Get OpenAI API credit** ($5-10 covers a full `bench tasks` run with GPT-4o-mini at default trials × ~15 questions).
2. Extract the two interfaces (`tokens.Counter`, `tasks.API`) — pure refactor, no behavior change. Maybe a half day.
3. Add `tasks.OpenAIAPI` with `chat/completions` + tool calls. Adapt the runner's message-construction step to dispatch on provider. ~1 day.
4. Add `tokens.OpenAICounter` with `tiktoken-go`. ~1 hour.
5. Replace `web_search_20250305` in Config A with a Tavily function tool. ~2 hours plus a Tavily account.
6. Add a `--provider {anthropic,openai}` flag wired through.
7. Run.

Total: ~2 days of focused work to get the first non-Anthropic run.

---

## 3. References

- Design doc: `docs/plans/2026-04-26-token-savings-benchmark-design.md`
- Implementation plan: `docs/plans/2026-04-26-token-savings-benchmark-impl.md`
- Branch: `bench/token-savings` (20 commits ahead of `main`)
- README section: see "Benchmarking Token Savings" in `README.md`

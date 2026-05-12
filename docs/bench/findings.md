# DocuMcp Benchmark Findings

This document summarizes what we learned while benchmarking DocuMcp with Codex CLI.

## Goal

The original goal was to measure whether DocuMcp can save tokens and improve answer speed compared with Codex native documentation lookup through web search.

The benchmark now compares:

- `native`: `codex --search exec ...`
- `documcp`: `codex exec ...` with DocuMcp attached as an MCP server
- `mcp_noop`: DocuMcp attached, but the prompt says to reply `OK` and not call tools

The no-op mode isolates fixed MCP attachment overhead from actual retrieval cost.

## What We Changed

Several changes improved benchmark validity and DocuMcp behavior:

- Added `cmd/bench`, `make bench`, and `make bench-run`.
- Added raw Codex JSONL capture in `bench-events/`.
- Added `mcp_noop` diagnostic mode.
- Isolated Codex runs with `--ignore-user-config`.
- Added sitemap-index support for web crawling.
- Added same-origin link-follow fallback for sites without useful sitemaps.
- Added a compact retrieval path:
  - `search_docs(query, source?, limit?, snippet_chars?)`
  - `get_page_excerpt(url, query?, max_chars?)`
  - `get_page(url)` remains full-page and backward compatible.

## Current Result

On public documentation sources such as Kubernetes, Go, and React, DocuMcp has not shown token savings against Codex native web search.

The strongest signal is from `mcp_noop`:

```text
input tokens:         ~19,201
cached input tokens:  often ~7,552
output tokens:        5
tool calls:           0
median duration:      ~8-9 seconds
```

This means attaching DocuMcp as an MCP server in Codex CLI currently has a fixed cost of roughly 19k input tokens before any useful retrieval happens.

## Interpretation

The remaining token cost is mostly not from DocuMcp result payloads. Search snippets and excerpts are now small. The overhead appears to come from Codex's MCP attachment path: tool metadata, schemas, descriptions, and agent/tool orchestration context.

DocuMcp can still improve latency for some multi-document questions, especially Kubernetes tasks, but token count remains higher because of the fixed MCP setup cost.

## Fair Conclusion

DocuMcp is not currently a token-saving replacement for Codex native web search on small questions against popular public documentation.

DocuMcp is still useful when the value is access and control rather than raw token savings:

- private docs
- internal wikis
- local or self-hosted documentation
- multiple curated sources available through one interface
- version-pinned documentation
- sources that native web search cannot access reliably
- repeatable, auditable retrieval from an indexed corpus

## Benchmark Caveats

The benchmark uses Codex CLI behavior, not a lower-level OpenAI API harness. Codex may include internal context, tool schemas, and MCP metadata that are not fully controllable by DocuMcp.

Success checks are substring-based and lightweight. They are useful for catching obvious failures but are not a full semantic evaluator.

Native web search has an advantage on popular public docs because the model and search tool already know or can quickly find common answers.

## Next Ideas

If we revisit token optimization later, the most promising experiments are:

- compact MCP endpoint exposing only `search_docs` and `get_page_excerpt`
- single dispatcher tool for all documentation actions
- API-level benchmark that bypasses Codex CLI overhead
- private/internal documentation benchmark where native web search cannot compete
- stronger answer-quality evaluator for multi-page questions

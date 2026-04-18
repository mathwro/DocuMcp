# Per-Project Documentation Source Scoping — Design

**Date:** 2026-04-18
**Status:** Approved, awaiting implementation plan

## Summary

Help AI assistants consistently scope documentation queries to the single source relevant to the current project. Two small changes: a nudge in the `search_docs` MCP tool description, and a README section showing users how to pin their consumer project's `CLAUDE.md` to a source name. Heavier server-side enforcement (named projects, per-project MCP endpoints) is captured as future work but not built.

## Problem

DocuMcp can register many documentation sources, but a given consumer project usually only needs one of them (e.g., the repo's own framework docs). Today, calls to `search_docs` without a `source` argument search every indexed source, which introduces noise from unrelated docs and wastes tokens.

The MCP tools already support scoping:
- `search_docs` accepts an optional `source` name.
- `browse_source` requires a `source` name.

So the tools are capable. The gap is that AI assistants forget to pass `source` unless told to. This spec closes that gap with the lightest possible intervention.

## Goals

- Make it easy for a user setting up DocuMcp in a new project to pin that project to one documentation source.
- Nudge the LLM client toward honoring that pin without user intervention.
- Capture heavier server-side alternatives as future work without building them.

## Non-goals

- No server-side enforcement of which sources a client may query.
- No DB schema changes, config fields, or new MCP tool parameters.
- No multi-source scoping (e.g., "project uses A + B but not C") — deferred to future work.
- No changes to `list_sources`, `browse_source`, or `get_page` descriptions.

## Design

Two small, independent changes.

### 1. Tool description nudge in `search_docs`

In `internal/mcp/tools.go`, append one sentence to the `search_docs` tool description:

> "If the calling project has a designated documentation source, always pass `source` to avoid noise from unrelated docs."

This string is sent to every MCP client and surfaces to the LLM on every tool listing, so it acts as a passive reminder whenever the client forgets to scope a call.

### 2. README section: "Scoping docs per project"

Add a new top-level section to `README.md` explaining the scoping pattern to human users. The section contains:

- A two-sentence rationale (sources are registered globally; a given project usually only queries one).
- A copy-paste snippet for the user to add to their consumer project's `CLAUDE.md`:

  ```
  When searching documentation via DocuMcp, always pass `source="<name>"`
  to `search_docs` and `browse_source`. Only deviate if the user asks.
  ```

- A one-line pointer to `list_sources` for discovering the valid name.

## Testing

- Add a unit test in `internal/mcp/` asserting the `search_docs` tool description contains the substring `"If the calling project has a designated documentation source"`. This prevents accidental removal of the nudge during future edits.
- README change is docs-only; no test.

## Future work (not built in this spec)

Captured so the options are recoverable if the client-side convention proves insufficient.

**Trigger to revisit:** either (a) users report the CLAUDE.md convention is being ignored in practice, or (b) a user needs multi-source scoping (project uses docs A + B but not C) that the single-source `source` parameter can't express.

### Option B — Named project concept in DocuMcp

- New DB table `projects(name TEXT PRIMARY KEY, allowed_source_ids JSON)`.
- Optional `project` parameter on `search_docs` and `browse_source`; when set, server filters results to the allowed source IDs.
- Web UI and config-file surface for defining projects.
- Pros: enforced server-side; supports multi-source scoping.
- Cons: new config surface, new tool arg, DB migration, UI work.

### Option C — Per-project MCP endpoints

- Route `/mcp/<project-name>` to a pre-scoped server instance.
- Consumer project's MCP client config points at the project-specific URL.
- Pros: no new tool arguments; enforced by URL.
- Cons: routing + config work; one-source-per-URL by default (multi-source would need the same project table as Option B).

## Open questions

None at spec time.

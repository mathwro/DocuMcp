# Per-Project Documentation Source Scoping — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Nudge AI assistants to scope documentation queries to one source per consumer project, via a one-sentence addition to the `search_docs` tool description plus a README section documenting the `CLAUDE.md` convention.

**Architecture:** Client-side only. No DB, config, or new tool arguments. Two independent edits — a Go string literal in `internal/mcp/tools.go` and a new Markdown section in `README.md`. One new Go test guards the nudge string from accidental removal.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v0.8.0, SQLite/FTS5 test fixtures in `internal/mcp/`.

**Spec:** `docs/plans/2026-04-18-per-project-doc-source-scoping-design.md`

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/mcp/tools.go` | Modify | Append nudge sentence to `search_docs` tool description (single string literal) |
| `internal/mcp/tools_description_test.go` | Create | Assert the `search_docs` description contains the nudge substring |
| `README.md` | Modify | Add new `## Scoping Docs Per Project` section between `## MCP Integration` and `## Development` |

Three discrete changes, three commits. TDD order: test → implementation → docs.

---

## Task 1: Add failing test for the nudge string

**Files:**
- Create: `internal/mcp/tools_description_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/tools_description_test.go` with:

```go
package mcp_test

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/documcp/documcp/internal/mcp"
)

// TestSearchDocsDescriptionContainsProjectNudge guards the one-sentence hint
// added to the search_docs tool description so AI clients are reminded to pass
// `source` when the calling project has a designated documentation source.
// See docs/plans/2026-04-18-per-project-doc-source-scoping-design.md.
func TestSearchDocsDescriptionContainsProjectNudge(t *testing.T) {
	store := openTestDB(t)
	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var desc string
	for _, tool := range res.Tools {
		if tool.Name == "search_docs" {
			desc = tool.Description
			break
		}
	}
	if desc == "" {
		t.Fatal("search_docs tool not found in ListTools response")
	}

	const needle = "If the calling project has a designated documentation source"
	if !strings.Contains(desc, needle) {
		t.Errorf("search_docs description missing project-scoping nudge.\nwant substring: %q\ngot: %s", needle, desc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestSearchDocsDescriptionContainsProjectNudge ./internal/mcp/ -v
```

Expected: **FAIL** with a message like `search_docs description missing project-scoping nudge.` The test compiles and runs (imports, helpers `openTestDB` / `connectTestClient` are already in `server_test.go`) but the substring is not yet present.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/mcp/tools_description_test.go
git commit -m "test(mcp): guard project-scoping nudge in search_docs description"
```

Rationale: committing the red test first keeps the TDD trail in history and lets the next task be a one-line diff.

---

## Task 2: Add the nudge to the `search_docs` tool description

**Files:**
- Modify: `internal/mcp/tools.go:77-91` (the `search_docs` tool registration block)

- [ ] **Step 1: Edit the `search_docs` description**

In `internal/mcp/tools.go`, locate the `search_docs` tool registration (around lines 76–91). The current description reads:

```go
Description: "Start here for any documentation question. Searches all indexed sources using " +
    "hybrid BM25 + semantic search and returns up to 10 results ranked by relevance. Each " +
    "result includes the source name, section path, and a short excerpt (~200 chars) centred " +
    "on the matched terms. If an excerpt confirms the page is relevant, call get_page with " +
    "that URL for the full content. Optionally restrict to a single source by name.",
```

Replace it with (one additional concatenated string on a new line at the end):

```go
Description: "Start here for any documentation question. Searches all indexed sources using " +
    "hybrid BM25 + semantic search and returns up to 10 results ranked by relevance. Each " +
    "result includes the source name, section path, and a short excerpt (~200 chars) centred " +
    "on the matched terms. If an excerpt confirms the page is relevant, call get_page with " +
    "that URL for the full content. Optionally restrict to a single source by name. " +
    "If the calling project has a designated documentation source, always pass `source` " +
    "to avoid noise from unrelated docs.",
```

Leave the rest of the block (`InputSchema`, handler) untouched.

- [ ] **Step 2: Run the guard test to verify it passes**

Run:
```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestSearchDocsDescriptionContainsProjectNudge ./internal/mcp/ -v
```

Expected: **PASS**.

- [ ] **Step 3: Run the full package test suite**

Run:
```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcp/
```

Expected: **PASS** — no existing test should assert the old description verbatim; a scan of `internal/mcp/server_test.go` confirms existing tests only check tool behavior, not description strings.

- [ ] **Step 4: Run the full module test suite**

Run:
```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Expected: **PASS**.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go
git commit -m "feat(mcp): nudge clients to pass source in search_docs description"
```

---

## Task 3: Add README section "Scoping Docs Per Project"

**Files:**
- Modify: `README.md` — insert a new section after the `## MCP Integration` block (which ends around line 165 with the tools table) and before `## Development` (around line 167).

- [ ] **Step 1: Insert the new section**

In `README.md`, find the line `## Development` (currently line 167). Immediately before it, insert this new section (keep one blank line above and below):

```markdown
## Scoping Docs Per Project

DocuMcp indexes documentation sources globally, but any given consumer project usually only needs one of them (for example, a Go service that should only ever search its own framework's docs). Searching every indexed source by default adds noise and wastes tokens.

To pin a project to a single source, add the following to that project's `CLAUDE.md` (or equivalent assistant instructions file):

```
When searching documentation via DocuMcp, always pass `source="<name>"` to
`search_docs` and `browse_source`. Only deviate if the user asks.
```

Replace `<name>` with the source name exactly as it appears in `list_sources`. Call `list_sources` once to discover the valid names.

This is a client-side convention — DocuMcp does not enforce the scope server-side. If you need enforced scoping (for example, multi-source projects or a team that can't rely on the convention being followed), see `docs/plans/2026-04-18-per-project-doc-source-scoping-design.md` for the deferred server-side options.
```

Note: the inner triple-backtick fence uses plain ` ``` ` (no language tag) so it renders as a literal snippet for copy-pasting. Make sure the outer fence pattern in the README is consistent with nearby code blocks (the repo already uses plain triple-backtick fences; no four-tick or tilde fences needed).

- [ ] **Step 2: Verify the README renders cleanly**

Run:
```bash
head -200 README.md | tail -60
```

Expected: The new `## Scoping Docs Per Project` heading appears between the MCP Integration tool table and the `## Development` heading, with the copy-paste snippet visible.

Optional quick lint (if `markdownlint-cli` is installed locally):
```bash
command -v markdownlint >/dev/null && markdownlint README.md || echo "skipping markdownlint (not installed)"
```

Expected: no errors. Skip silently if the tool is not present.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add README section on scoping docs per project"
```

---

## Task 4: Final verification

**Files:** none modified; verification only.

- [ ] **Step 1: Full build**

Run:
```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
```

Expected: exit code 0, no output.

- [ ] **Step 2: Full test suite**

Run:
```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Expected: all packages PASS, including `internal/mcp` which now has `TestSearchDocsDescriptionContainsProjectNudge`.

- [ ] **Step 3: Confirm git log matches the plan**

Run:
```bash
git log --oneline -5
```

Expected top-of-log (newest first):
1. `docs: add README section on scoping docs per project`
2. `feat(mcp): nudge clients to pass source in search_docs description`
3. `test(mcp): guard project-scoping nudge in search_docs description`
4. `docs(plans): design spec for per-project doc source scoping` (already committed on `main`)

- [ ] **Step 4: Nothing else**

No CI push, no PR, no deploy in this plan. Stop here and report back.

---

## Out of Scope (do not implement)

The spec's "Future work" section lists Option B (named projects in DB) and Option C (per-project MCP endpoints). **Do not implement either in this plan.** They are captured in the design doc so they are recoverable if the client-side convention proves insufficient.

# DocuMcp Design Document
**Date:** 2026-02-27

> **Historical record — not authoritative.** This is the original design as written before implementation. Several assumptions have since changed; see `README.md`, `docs/configuration.md`, and `docs/sources.md` for current behavior. Notable drift: `config.yaml` is now optional (built-in defaults apply when missing); the Web UI persists sources to SQLite (not back to `config.yaml`); GitHub auth uses user-supplied fine-grained PATs (not the device-code flow this doc describes).

## Overview

DocuMcp is a locally-hosted MCP (Model Context Protocol) server that indexes documentation from multiple sources and makes it quickly available to AI coding assistants. It runs as a single Docker/Podman container, is configured via a simple YAML file, and exposes indexed documentation through semantic search, full-text search, and hierarchical browsing.

The core problem it solves: AI assistants often fail to find relevant documentation because they don't know it exists or phrase queries with different terminology than the docs use. DocuMcp gives the AI both a map of what documentation exists and the ability to find content conceptually — not just by keyword.

---

## Architecture

Single Go binary, all-in-one container. Three concurrent subsystems:

```
┌─────────────────────────────────────────────────┐
│                  DocuMcp Container               │
│                                                 │
│  ┌─────────────┐   ┌──────────────────────────┐ │
│  │  MCP Server │   │     Web UI + REST API    │ │
│  │ (stdio/SSE) │   │     (embedded static)    │ │
│  └──────┬──────┘   └───────────┬──────────────┘ │
│         │                      │                 │
│         └──────────┬───────────┘                 │
│                    ▼                             │
│           ┌────────────────┐                     │
│           │   Core Engine  │                     │
│           │ Search | Index │                     │
│           └───────┬────────┘                     │
│                   │                              │
│         ┌─────────┼──────────┐                   │
│         ▼         ▼          ▼                   │
│     [GitHub]  [Confluence] [Web/Astro]  ...      │
│      Adapter   Adapter     Adapter               │
│                   │                              │
│         ┌─────────▼──────────┐                   │
│         │  SQLite (FTS5 +    │                   │
│         │   sqlite-vec)      │                   │
│         │  /data/documcp.db  │                   │
│         └────────────────────┘                   │
└─────────────────────────────────────────────────┘
         │                    │
    Claude Code          Browser
    (MCP client)       (Web UI :8080)
```

**Tech stack:**
- Language: Go
- MCP SDK: `github.com/modelcontextprotocol/go-sdk` v1.4.0 (official, Anthropic + Google maintained)
- Database: SQLite with FTS5 (full-text) and `sqlite-vec` (vector similarity)
- Embeddings: `all-MiniLM-L6-v2` ONNX model (~90MB) bundled in image — zero user setup
- Web UI: Embedded static files, dark mode, minimal developer-tool aesthetic
- Distribution: Single Docker image via GitHub Container Registry, Podman compatible

---

## Indexing & Search

When a source is onboarded or re-crawled on schedule:

1. Crawl all pages (sitemap.xml first, link-following fallback)
2. Strip markup, extract clean text + title + URL + hierarchy path
3. Store in SQLite with two parallel indexes:
   - **FTS5** for keyword/BM25 search
   - **sqlite-vec** for vector similarity search
4. Generate embeddings using the bundled ONNX model (runs in-process)

At query time, results from both indexes are merged using reciprocal rank fusion before returning to the AI.

### MCP Tools

```
search_docs(query, source?)     → ranked results (hybrid BM25 + semantic)
browse_source(source_name)      → top-level sections with page counts
browse_source(source, section)  → pages within a specific section
get_page(url)                   → full page content
list_sources()                  → all configured sources + crawl status
```

### Token-aware browsing

`browse_source` is hierarchical and lazy — the AI navigates in layers:

1. Call `browse_source("Team Confluence")` → get top-level sections (cheap, ~10-20 tokens per entry)
2. Call `browse_source("Team Confluence", section="Authentication")` → get pages in that section
3. Call `get_page(url)` → fetch the specific page

This prevents large sites from flooding the AI's context while still giving it full awareness of what documentation exists.

### Future: Semantic search enhancement

The ONNX embedding model is included in v1. Future enhancement: expose configurable embedding model selection for users who want higher quality embeddings (e.g. a larger model). The interface is already in place.

---

## Source Adapters

Each adapter implements a common Go interface, making new sources addable without touching the core engine:

```go
type SourceAdapter interface {
    Crawl(ctx context.Context, source Source) (<-chan Page, error)
    BuildHierarchy(pages []Page) PageTree
    Authenticate(ctx context.Context) (Token, error)
}
```

### v1 Adapters

| Adapter | Auth method | Crawl strategy |
|---|---|---|
| **GitHub Wiki** | Device code flow (github.com/login/device) | GitHub REST API |
| **Azure DevOps Wiki** | Device code flow (microsoft.com/devicelogin) | Azure DevOps REST API |
| **Confluence** | TBD — likely pre-registered DocuMcp app, user-level consent only | Confluence REST API, space-scoped |
| **Generic Web** (public) | None / Basic auth | Sitemap.xml first, link-following fallback |
| **Generic Web** (Azure AD) | Device code flow (microsoft.com/devicelogin) | Sitemap.xml first, link-following fallback |

### v2 Candidates (interface already supports)
Notion, GitLab Wiki, Docusaurus, ReadTheDocs.

---

## Authentication

### Device code flow (GitHub, Azure AD)

No app registration required from the user. No IT admin consent required. Flow:

1. User clicks "Connect" in Web UI
2. DocuMcp displays a short code and URL (e.g. `microsoft.com/devicelogin` or `github.com/login/device`)
3. User opens URL in any browser, enters code, logs in with their account
4. DocuMcp receives and stores the token
5. Tokens are refreshed automatically; re-auth only needed if token is revoked

For users who already have `az` or `gh` CLI installed and authenticated on the host, credential dirs can be mounted to skip the flow entirely:

```yaml
volumes:
  - ~/.azure:/root/.azure:ro       # optional: skip Azure device flow
  - ~/.config/gh:/root/.config/gh:ro  # optional: skip GitHub device flow
```

### Confluence (TBD)

Atlassian's OAuth 2.0 does not have the same corporate admin consent friction as Azure AD. A DocuMcp-registered Atlassian OAuth app (client ID baked into binary, user-level authorization only) is the likely approach. To be validated against a live Confluence environment before implementation.

### Token storage

OAuth tokens are stored encrypted in the SQLite database. They are never written to `config.yaml`. Only non-secret identifiers (source URLs, space keys, repo names) live in the config file.

---

## Configuration

`config.yaml` is the source of truth. The Web UI can read and modify it. DocuMcp watches for file changes and reloads without restart.

```yaml
server:
  port: 8080
  data_dir: /app/data

sources:
  - name: "Team Confluence"
    type: confluence
    base_url: "https://company.atlassian.net"
    space_key: "TEAM"
    crawl_schedule: "0 0 * * *"      # cron, daily at midnight

  - name: "Platform Docs"
    type: web
    url: "https://docs.internal.company.com"
    auth: azure_ad
    crawl_schedule: "0 */6 * * *"    # every 6 hours

  - name: "API Reference"
    type: github_wiki
    repo: "myorg/myrepo"
    crawl_schedule: "0 0 * * 1"      # weekly, monday
```

- `crawl_schedule` is optional — omitting it means manual crawl only
- No secrets in the config file
- Adding a source via the Web UI writes to this file; hand-edits are equally valid

---

## Web UI

Served by the Go binary on `:8080` via embedded static files. Dark mode themed. Three main screens:

**Sources dashboard** (home)
- All sources with crawl status, last crawled timestamp, page count, index size
- Per-source actions: crawl now, re-index, disconnect auth, remove
- Visual indicator when a source needs authentication

**Add/Edit source**
- Form that writes to `config.yaml`
- Source type selector reveals relevant fields
- Inline auth connection — triggers device code flow, shows code/URL to visit
- Crawl schedule picker

**Search** (debug/testing)
- Query the index directly to verify indexing worked
- Shows source, relevance score, and hierarchy path per result

---

## Docker Deployment

```bash
# Minimal
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v ./config.yaml:/app/config.yaml \
  -v documcp_data:/app/data \
  ghcr.io/documcp/documcp:latest

# With optional CLI credential mounts
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v ./config.yaml:/app/config.yaml \
  -v documcp_data:/app/data \
  -v ~/.azure:/root/.azure:ro \
  -v ~/.config/gh:/root/.config/gh:ro \
  ghcr.io/documcp/documcp:latest
```

**Docker Compose:**

```yaml
services:
  documcp:
    image: ghcr.io/documcp/documcp:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - documcp_data:/app/data
      - ~/.azure:/root/.azure:ro        # optional
      - ~/.config/gh:/root/.config/gh:ro  # optional
    restart: unless-stopped

volumes:
  documcp_data:
```

**MCP client configuration** (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "documcp": {
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

Podman compatible — no daemon dependency.

---

## Future Enhancements

- **Semantic search quality**: Configurable/swappable embedding model for users wanting higher quality
- **Additional adapters**: Notion, GitLab Wiki, Docusaurus, ReadTheDocs
- **Confluence auth**: Validate and finalize auth approach against live environment
- **Incremental crawling**: Only re-index changed pages (via ETags or last-modified headers)
- **Multi-user support**: Currently single-user local tool; multi-user would require per-user auth token isolation

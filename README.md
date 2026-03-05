# DocuMcp

A self-hosted MCP (Model Context Protocol) server that indexes your documentation and makes it searchable by AI coding assistants. Runs as a single Docker/Podman container with a built-in web UI for managing sources.

## Features

- **Hybrid search** — combines BM25 full-text search and semantic vector search (via `all-MiniLM-L6-v2`) merged with reciprocal rank fusion
- **Multiple source types** — public web docs, GitHub Wikis, Azure DevOps Wikis
- **Authenticated sources** — device code flows for GitHub and Microsoft (no app registration required)
- **Scheduled crawling** — cron-based re-indexing keeps docs fresh
- **Web UI** — manage sources, trigger crawls, and test searches from a browser
- **MCP tools** — `search_docs`, `list_sources`, `browse_source`, `get_page`
- **Zero external dependencies** — SQLite with FTS5 + sqlite-vec, ONNX model bundled in the container image

## Quick Start

### 1. Build the container image

```bash
make docker
# or: podman build -t documcp:local .
```

> The build downloads the `all-MiniLM-L6-v2` ONNX model (~90 MB) from HuggingFace. Requires Python/pip for `optimum-cli` in the builder stage.

### 2. Create a config file

```yaml
# config.yaml
server:
  port: 8080
  data_dir: /app/data

sources:
  - name: My Docs
    type: web
    url: https://docs.example.com/
    crawl_schedule: "@weekly"
```

### 3. Generate a secret key

```bash
openssl rand -hex 32
```

Create a `.env` file (never commit this):

```
DOCUMCP_SECRET_KEY=<your-hex-key>
```

### 4. Start the server

```bash
docker compose up -d
# or: podman compose up -d
```

Open `http://localhost:8080` to manage sources and search.

## Configuration

### `config.yaml`

| Field | Description |
|---|---|
| `server.port` | HTTP port (default: `8080`) |
| `server.data_dir` | SQLite database directory |
| `sources[].name` | Display name for the source |
| `sources[].type` | `web`, `github_wiki`, or `azure_devops` |
| `sources[].url` | Base URL (web and Azure DevOps sources) |
| `sources[].repo` | `owner/repo` (GitHub Wiki sources) |
| `sources[].crawl_schedule` | Cron expression, e.g. `0 2 * * *` or `@weekly` |

### Environment Variables

| Variable | Description |
|---|---|
| `DOCUMCP_SECRET_KEY` | 32-byte hex key for encrypting stored OAuth tokens. If unset, a random key is generated per run (tokens lost on restart). |
| `DOCUMCP_API_KEY` | Bearer token required on `/api/*` and `/mcp/*` endpoints. If unset, all endpoints are unauthenticated (warns at startup). |
| `DOCUMCP_CONFIG` | Path to config file (default: `config.yaml`) |
| `DOCUMCP_MODEL_PATH` | Path to the ONNX model directory |

## Source Types

### Web (`type: web`)

Crawls public websites. Discovers pages via sitemap, falls back to link following. Polite crawling with 500 ms delay between requests and `Retry-After` backoff on HTTP 429.

```yaml
- name: Harbor Docs
  type: web
  url: https://goharbor.io/docs/2.14.0/
  crawl_schedule: "@weekly"
```

### GitHub Wiki (`type: github_wiki`)

Indexes a GitHub repository's wiki. Requires authentication via GitHub device code flow — click **Connect** in the Web UI and follow the prompts.

```yaml
- name: My Project Wiki
  type: github_wiki
  repo: owner/repo
  crawl_schedule: "@daily"
```

> **Shortcut:** Mount `~/.config/gh` into the container to reuse existing `gh` CLI credentials and skip the device flow.

### Azure DevOps Wiki (`type: azure_devops`)

Indexes an Azure DevOps wiki. Authenticates via Microsoft device code flow using the Azure CLI client ID (no admin consent required).

```yaml
- name: Team Wiki
  type: azure_devops
  url: https://dev.azure.com/org/project
  crawl_schedule: "@weekly"
```

> **Shortcut:** Mount `~/.azure` into the container to reuse existing Azure CLI credentials.

## MCP Integration

The MCP server is available at `http://localhost:8080/mcp/` using Server-Sent Events (SSE) transport.

### Claude Desktop (`claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "documcp": {
      "url": "http://localhost:8080/mcp/",
      "headers": {
        "Authorization": "Bearer <your-api-key>"
      }
    }
  }
}
```

### Available MCP Tools

| Tool | Description |
|---|---|
| `list_sources` | Lists all configured sources with crawl status |
| `search_docs(query, source?)` | Hybrid search across all (or a specific) source |
| `browse_source(source, section?)` | Hierarchical table of contents — drill into sections |
| `get_page(url)` | Retrieve full content of a specific page |

## Development

### Requirements

- Go 1.21+ with CGo enabled
- GCC / `libsqlite3-dev` (for `go-sqlite3`)

### Build & Test

```bash
make build   # CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
make test    # CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
make lint    # requires golangci-lint
```

> Both `CGO_ENABLED=1` and `-tags sqlite_fts5` are required — SQLite FTS5 and sqlite-vec use CGo.

### Project Layout

```
cmd/documcp/          # main binary
internal/
  adapter/            # source adapters (web, github, azuredevops)
  api/                # REST API handlers and server
  auth/               # device code flows, encrypted token store
  config/             # YAML config + file watcher
  crawler/            # crawl orchestrator + cron scheduler
  db/                 # SQLite schema, CRUD, FTS5, tokens
  embed/              # ONNX embedding model wrapper
  mcp/                # MCP server + 4 tool handlers
  search/             # FTS5, semantic search, RRF, browse
web/static/           # embedded HTML/JS/CSS (Alpine.js, dark theme)
models/               # ONNX model (downloaded at Docker build time)
```

## License

GPL-3.0 — see [LICENSE](LICENSE).

# DocuMcp

A self-hosted MCP (Model Context Protocol) server that indexes your documentation and makes it searchable by AI coding assistants. Runs as a single Docker/Podman container with a built-in web UI for managing sources.

## Features

- **Hybrid search** — combines BM25 full-text search and semantic vector search (via `all-MiniLM-L6-v2`) merged with reciprocal rank fusion
- **Multiple source types** — public web docs, GitHub Wikis, GitHub repositories, Azure DevOps Wikis
- **Authenticated sources** — user-supplied fine-grained PATs for GitHub, Microsoft device code flow for Azure DevOps (no app registration required)
- **Scheduled crawling** — cron-based re-indexing keeps docs fresh
- **Web UI** — manage sources, trigger crawls, and test searches from a browser
- **MCP tools** — `search_docs`, `list_sources`, `browse_source`, `get_page`
- **Zero external dependencies** — SQLite with FTS5 + sqlite-vec, ONNX model bundled in the container image

## Quick Start — pre-built image

No clone required. Works with `docker` or `podman` (substitute the command as needed).

### 1. Create a working directory with a config file

```bash
mkdir documcp && cd documcp

cat > config.yaml <<'EOF'
server:
  port: 8080
  data_dir: /app/data
sources: []
EOF
```

`config.yaml` is the source of truth for sources and schedules. You can edit it directly or manage sources from the Web UI — the file watcher picks up changes without a restart.

### 2. Generate a persistent secret key

```bash
echo "DOCUMCP_SECRET_KEY=$(openssl rand -hex 32)" > .env
```

This key encrypts GitHub PATs and Azure DevOps tokens in SQLite. Keep the `.env` file — regenerating the key invalidates any stored tokens.

### 3. Run the container

```bash
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v "$PWD/config.yaml:/app/config.yaml" \
  -v documcp-data:/app/data \
  --env-file .env \
  ghcr.io/mathwro/documcp:latest
```

Open `http://localhost:8080` to manage sources and search.

> **Updating:** `docker pull ghcr.io/mathwro/documcp:latest && docker rm -f documcp` then re-run step 3. The named `documcp-data` volume preserves your indexed content and tokens across upgrades.
>
> **Pinning a version:** replace `:latest` with `:0.1.0` (or `:0`, `:0.1`) to pin to a specific release. Image tags track published GitHub releases.

## Quick Start — from source

Clone the repo if you want to modify the code or use the Makefile / compose shortcuts.

```bash
git clone https://github.com/mathwro/DocuMcp.git
cd DocuMcp

make init            # generates config.yaml and .env
make docker          # builds documcp:local (downloads the ONNX model)
docker compose up -d # or: podman compose up -d
```

> `make init` skips files that already exist, so it is safe to re-run. The build downloads the `all-MiniLM-L6-v2` ONNX model (~90 MB) from HuggingFace via `optimum-cli` in the builder stage.

## Configuration

### `config.yaml`

| Field | Description |
|---|---|
| `server.port` | HTTP port (default: `8080`) |
| `server.data_dir` | SQLite database directory |
| `sources[].name` | Display name for the source |
| `sources[].type` | `web`, `github_wiki`, `github_repo`, or `azure_devops` |
| `sources[].url` | Base URL (web and Azure DevOps sources) |
| `sources[].include_path` | For `web`: restricts crawling to a URL prefix (same origin required). For `github_repo`: restricts indexing to a subfolder (e.g. `docs/`). `..` segments are rejected. |
| `sources[].repo` | `owner/repo` (GitHub Wiki and GitHub Repo sources) |
| `sources[].branch` | Branch name for `github_repo` sources (default: `main`) |
| `sources[].crawl_schedule` | Cron expression, e.g. `0 2 * * *` or `@weekly` |

### Environment Variables

| Variable | Description |
|---|---|
| `DOCUMCP_SECRET_KEY` | 32-byte hex key for encrypting stored GitHub PATs and Azure DevOps OAuth tokens in SQLite. If unset, a random key is generated per run (tokens lost on restart). |
| `DOCUMCP_API_KEY` | Bearer token required on `/api/*` and `/mcp/*` endpoints. If unset, all endpoints are unauthenticated (warns at startup). |
| `DOCUMCP_CONFIG` | Path to config file (default: `config.yaml`) |
| `DOCUMCP_MODEL_PATH` | Path to the ONNX model directory |
| `DOCUMCP_BIND_ADDR` | Address to listen on. Defaults to `127.0.0.1:<port>` so a fresh install is not reachable from the network. The Docker image sets this to `0.0.0.0:8080` so the container's port mapping works; when running the binary directly, set `DOCUMCP_BIND_ADDR=0.0.0.0:8080` to expose it on the LAN. |

## Source Types

### Web (`type: web`)

Crawls public websites. Discovers pages via sitemap, falls back to link following. Polite crawling with 500 ms delay between requests and `Retry-After` backoff on HTTP 429.

```yaml
- name: ArgoCD Operator Manual
  type: web
  url: https://argo-cd.readthedocs.io/en/stable/
  include_path: https://argo-cd.readthedocs.io/en/stable/operator-manual/
  crawl_schedule: "@weekly"
```

### GitHub Wiki (`type: github_wiki`)

Indexes a GitHub repository's wiki. Public wikis work without authentication. For private wikis, click **Connect** in the Web UI and paste a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) with **Contents: Read-only** on the target repo.

```yaml
- name: My Project Wiki
  type: github_wiki
  repo: owner/repo
  crawl_schedule: "@daily"
```

### GitHub Repo (`type: github_repo`)

Indexes Markdown (`.md`, `.mdx`) and text (`.txt`) files directly from a repository's tree via the GitHub tarball endpoint. Files larger than 5 MiB are skipped. Use `include_path` to restrict indexing to a subfolder such as `docs/`.

```yaml
- name: My Project Docs
  type: github_repo
  repo: owner/repo
  branch: main
  include_path: docs/
  crawl_schedule: "@daily"
```

Public repos work without authentication. For private repos, click **Connect** in the Web UI and paste a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with **Contents: Read-only** on the target repo.

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

### Claude Code (`.mcp.json` in your project root)

```json
{
  "mcpServers": {
    "documcp": {
      "type": "sse",
      "url": "http://localhost:8080/mcp/",
      "headers": {
        "Authorization": "Bearer <your-api-key>"
      }
    }
  }
}
```

Omit the `headers` block when `DOCUMCP_API_KEY` is unset. Restart Claude Code so it picks up the config change; run `/mcp` to confirm the server is connected.

### Available MCP Tools

| Tool | Description |
|---|---|
| `list_sources` | Lists all configured sources with names, types, page counts, and crawl status |
| `search_docs(query, source?)` | Hybrid search returning up to 10 results with short excerpts (~200 chars) and source names. Use `get_page` for full content. |
| `browse_source(source, section?)` | Hierarchical table of contents — top-level sections with page counts, or up to 50 pages in a section |
| `get_page(url)` | Retrieve the full content of a specific page by URL |

## Development

### Requirements

- Go 1.26+ with CGo enabled
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
  adapter/            # source adapters (web, github, githubrepo, azuredevops)
  api/                # REST API handlers and server
  auth/               # Microsoft device code flow, encrypted token store
  config/             # YAML config + file watcher
  crawler/            # crawl orchestrator + cron scheduler
  db/                 # SQLite schema, CRUD, FTS5, tokens
  embed/              # ONNX embedding model wrapper
  mcp/                # MCP server + 4 tool handlers
  search/             # FTS5, semantic search, RRF, browse
web/static/           # embedded HTML/JS/CSS (Alpine.js, dark theme)
models/               # ONNX model (downloaded at Docker build time)
```

## Troubleshooting

**`/api/*` or `/mcp/*` returns 401 `unauthorized`.**
`DOCUMCP_API_KEY` is set — every request needs `Authorization: Bearer <key>`. Unset it for local-only use, or pass the header from your MCP client.

**The UI loads but `/api/sources` returns 404 in docker-compose.**
Check `docker compose logs documcp`. The binary inside the container binds to `0.0.0.0:8080` (the image sets `DOCUMCP_BIND_ADDR`); if you changed the port in `config.yaml`, also update the compose `ports:` mapping and — if running outside compose — set `DOCUMCP_BIND_ADDR=0.0.0.0:<new-port>`.

**Stored GitHub / Azure DevOps tokens fail to decrypt after a restart.**
`DOCUMCP_SECRET_KEY` either changed or was never set. A missing key means DocuMcp generates an ephemeral key per run. Re-auth the source in the UI and set a persistent key. Generate one with `openssl rand -hex 32`.

**Semantic search returns no results but keyword search works.**
The ONNX embedding model failed to load. Check startup logs for `embedding model not loaded` — usually `DOCUMCP_MODEL_PATH` points to a missing directory. The container image includes the model at `/app/models/all-MiniLM-L6-v2`; for a bare-binary install, export the model yourself with [optimum-cli](https://huggingface.co/docs/optimum/exporters/onnx/usage_guides/export_a_model).

**A source shows 0 pages after a crawl.**
Check the server logs. Common causes: GitHub PAT missing **Contents: Read-only** on the target repo (manifests as `unauthorized — token missing or lacks repo scope`); `include_path` with a typo so no file matches; the web source's sitemap is outside the `include_path` prefix. Trigger **Crawl Now** in the UI and watch the progress badge — it shows `N / Total pages` during a run.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development loop, code style, and what I'm likely to accept or push back on.

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

GPL-3.0 — see [LICENSE](LICENSE).

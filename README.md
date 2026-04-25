# DocuMcp

A self-hosted MCP (Model Context Protocol) server that indexes your documentation and makes it searchable by AI coding assistants. Built primarily as a **local-first tool** — runs as a single Docker / Podman container on your workstation, with a built-in web UI for managing sources.

## Features

- **Hybrid search** — combines BM25 full-text search and semantic vector search (via `all-MiniLM-L6-v2`) merged with reciprocal rank fusion
- **Multiple source types** — public web docs, GitHub Wikis, GitHub repositories, Azure DevOps Wikis
- **Authenticated sources** — user-supplied fine-grained PATs for GitHub, Microsoft device code flow for Azure DevOps (no app registration required)
- **Scheduled crawling** — cron-based re-indexing keeps docs fresh
- **Web UI** — manage sources, trigger crawls, and test searches from a browser
- **MCP tools** — `search_docs`, `list_sources`, `browse_source`, `get_page`
- **Zero external dependencies** — SQLite with FTS5 + sqlite-vec, ONNX model bundled in the container image

## Quick Start

The fastest way to get running — single user, container on `localhost`, no config file or API key needed.

```bash
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v documcp-data:/app/data \
  ghcr.io/mathwro/documcp:latest
```

Open `http://localhost:8080`. Add a public web source from the UI and trigger a crawl. The `documcp-data` named volume preserves your indexed content and any tokens you store across restarts.

For private sources, exposing the port to your LAN, declarative source seeding via `config.yaml`, or building from source, see **[docs/install.md](docs/install.md)**.

## Documentation

| Topic | Page |
|---|---|
| Installation, updating, when to enable the API key | [docs/install.md](docs/install.md) |
| `config.yaml` fields and environment variables | [docs/configuration.md](docs/configuration.md) |
| Source types (web, GitHub Wiki, GitHub Repo, Azure DevOps) | [docs/sources.md](docs/sources.md) |
| Connecting Claude Desktop / Claude Code, MCP tools reference | [docs/mcp-clients.md](docs/mcp-clients.md) |
| Common problems and how to diagnose them | [docs/troubleshooting.md](docs/troubleshooting.md) |
| Build, test, project layout | [docs/development.md](docs/development.md) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development loop, code style, and what I'm likely to accept or push back on.

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

GPL-3.0 — see [LICENSE](LICENSE).

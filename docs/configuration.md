# Configuration

DocuMcp reads its configuration from an optional YAML file and a small set of environment variables. If no config file is present, the server starts with built-in defaults (`port: 8080`, `data_dir: /app/data`, no declarative sources) and all source management happens through the Web UI — those sources persist in the SQLite database under `data_dir`.

Provide a `config.yaml` when you want to declare sources and crawl schedules in version control. Config-declared sources are mirrored into SQLite so they appear in the Web UI and API source listings. The file watcher reloads YAML on save, so source changes take effect without a restart. UI-added sources are stored in the database, not written back to YAML.

`DOCUMCP_CONFIG` lets you override the path. When it is set, the file must exist (typos fail loudly); when it is unset, a missing file silently falls back to defaults.

## `config.yaml`

| Field | Description |
|---|---|
| `server.port` | HTTP port (default: `8080`) |
| `server.data_dir` | SQLite database directory |
| `sources[].name` | Display name for the source |
| `sources[].type` | `web`, `github_wiki`, `github_repo`, or `azure_devops` |
| `sources[].url` | Base URL (web and Azure DevOps sources) |
| `sources[].include_path` | Legacy single path filter. Prefer `include_paths` for new config. |
| `sources[].include_paths` | For `web`: only same-origin URLs under these prefixes are indexed. Entries may be relative paths such as `/guides/` or full same-origin URLs. For `github_repo`: only files under these repository folders are indexed. Empty means the current whole-source behavior. |
| `sources[].repo` | `owner/repo` (GitHub Wiki and GitHub Repo sources) |
| `sources[].branch` | Branch name for `github_repo` sources (default: `main`) |
| `sources[].crawl_schedule` | Cron expression, e.g. `0 2 * * *` or `@weekly` |

See [sources.md](sources.md) for a per-type breakdown with examples.

## Environment Variables

| Variable | Description |
|---|---|
| `DOCUMCP_SECRET_KEY` | 32-byte hex key for encrypting stored GitHub PATs and Azure DevOps OAuth tokens in SQLite. If unset, a random key is generated per run (tokens lost on restart). Only relevant if you use authenticated sources. |
| `DOCUMCP_API_KEY` | Bearer token required on `/api/*` and `/mcp/*` endpoints. If unset, all endpoints are unauthenticated (warns at startup). See [install.md](install.md#when-to-set-documcp_api_key) for when to enable this. |
| `DOCUMCP_CONFIG` | Path to config file (default: `config.yaml`) |
| `DOCUMCP_MODEL_PATH` | Path to the ONNX model directory |
| `DOCUMCP_BIND_ADDR` | Address to listen on. Defaults to `127.0.0.1:<port>` so a fresh install is not reachable from the network. The Docker image sets this to `0.0.0.0:8080` so the container's port mapping works; when running the binary directly, set `DOCUMCP_BIND_ADDR=0.0.0.0:8080` to expose it on the LAN. |

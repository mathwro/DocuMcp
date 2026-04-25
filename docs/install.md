# Installation

DocuMcp is built primarily as a **local-first tool** — a single container running on your workstation (or a trusted machine on your LAN) so your AI coding assistant can search your documentation. By default the binary binds to `127.0.0.1` and the API endpoints are unauthenticated; both choices assume you are the only user.

Two install paths are supported. Pick one.

- **Pre-built image** — fastest, no clone, just Docker / Podman.
- **From source** — needed if you want to modify the code or use the Makefile / compose shortcuts.

## Quick Start — pre-built image

Works with `docker` or `podman` (substitute the command as needed).

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

This key encrypts GitHub PATs and Azure DevOps tokens in SQLite. Keep the `.env` file — regenerating the key invalidates any stored tokens. You only need this if you plan to add authenticated sources (private GitHub repos/wikis or Azure DevOps); for purely public sources it can be omitted.

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

```bash
git clone https://github.com/mathwro/DocuMcp.git
cd DocuMcp

make init            # generates config.yaml and .env
make docker          # builds documcp:local (downloads the ONNX model)
docker compose up -d # or: podman compose up -d
```

> `make init` skips files that already exist, so it is safe to re-run. The build downloads the `all-MiniLM-L6-v2` ONNX model (~90 MB) from HuggingFace via `optimum-cli` in the builder stage.

## When to set `DOCUMCP_API_KEY`

`DOCUMCP_API_KEY` is a bearer token enforced on every `/api/*` and `/mcp/*` request. It is **off by default** because the typical deployment is a single-user container bound to `127.0.0.1`, where the host's own permissions are the security boundary.

| Situation | API key needed? |
|---|---|
| Single user, container on `localhost`, default `127.0.0.1` bind | No |
| Exposing the port to your LAN (`DOCUMCP_BIND_ADDR=0.0.0.0:8080`) | **Yes** |
| Running on a shared workstation or jump host | **Yes** |
| Behind a reverse proxy that enforces its own auth | Optional |
| Reachable from the public internet | **Yes** (and put it behind TLS + a proxy) |

When you do enable it, generate a long random value and add it to `.env`:

```bash
echo "DOCUMCP_API_KEY=$(openssl rand -hex 32)" >> .env
```

Then include it in every MCP client config — see [mcp-clients.md](mcp-clients.md). The startup log warns if the key is unset, which is expected for the local-only case.

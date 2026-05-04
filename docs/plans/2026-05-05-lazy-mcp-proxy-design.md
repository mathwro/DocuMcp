# Lazy MCP Proxy Design

## Problem

DocuMcp currently runs as a Docker or Podman container and exposes MCP over local HTTP:

- SSE at `http://localhost:8080/mcp/sse`
- Streamable HTTP at `http://localhost:8080/mcp/http`

That works well when the container is already running. The rough edge is AI tools that load MCP servers at the start of every session. If DocuMcp is configured as a URL server and the container is stopped, those tools may show a connection error even when the user does not need DocuMcp in that session.

The goal is to let users keep DocuMcp stopped by default, avoid startup noise in AI tools, and still make DocuMcp available on demand.

## Goals

- Provide a command-based MCP entrypoint for clients that support stdio MCP servers.
- Preserve the current install story: users should be able to pull `ghcr.io/mathwro/documcp:latest` and configure their AI tool without cloning the repository or installing a host binary.
- Start cleanly even when the DocuMcp container is stopped.
- Lazily start the full DocuMcp HTTP server inside the command container only when a DocuMcp tool is actually called.
- Keep the container image as the main runtime so users keep the bundled model, SQLite setup, web UI, crawlers, and existing install story.
- Return clear MCP tool errors when the inner DocuMcp server cannot start.
- Avoid writing secrets or changing local configuration during normal proxy startup.

## Non-Goals

- Replace the container install path.
- Add a second full bare-metal server mode.
- Require users to clone the repository or install a separate host-side CLI.
- Require mounting the Docker or Podman socket into the container.
- Hide API key requirements from users who enabled `DOCUMCP_API_KEY`.
- Remove the existing URL-based MCP configuration for clients that handle it well.

## Proposed Shape

Add a command mode to the existing containerized `documcp` binary:

```bash
documcp mcp-proxy
```

The published image can then be used directly as a command-based MCP server:

```bash
docker run --rm -i \
  -v documcp-data:/app/data \
  ghcr.io/mathwro/documcp:latest \
  mcp-proxy
```

Podman uses the same shape:

```bash
podman run --rm -i \
  -v documcp-data:/app/data \
  ghcr.io/mathwro/documcp:latest \
  mcp-proxy
```

This requires the runtime image to use `ENTRYPOINT ["documcp"]` instead of only `CMD ["documcp"]`, so Docker/Podman command arguments become DocuMcp subcommands. Running the image without extra arguments must continue to start the normal server.

The `mcp-proxy` command speaks MCP over stdio to the AI client. It presents the same tool surface as the HTTP MCP server:

- `list_sources`
- `search_docs`
- `browse_source`
- `get_page`

On process startup, the proxy initializes MCP stdio and advertises tools, but it does not start the full HTTP server yet. On the first tool call, it starts the normal DocuMcp server inside the same container, waits for the local HTTP MCP endpoint to become healthy, then forwards the request.

Example client config:

```json
{
  "mcpServers": {
    "documcp": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-v",
        "documcp-data:/app/data",
        "ghcr.io/mathwro/documcp:latest",
        "mcp-proxy"
      ]
    }
  }
}
```

This keeps the setup flow to "pull/run the image" rather than "clone/build/install a local binary".

## Runtime Behavior

### Startup

The AI client starts the container with `docker run -i` or `podman run -i`. Inside that container, the proxy starts as a lightweight stdio MCP server. Startup must not start the full DocuMcp HTTP server, load the embedding model, open crawlers, or connect to `localhost:8080`. This is what prevents session-start errors and avoids running the heavier server when DocuMcp is not needed.

The proxy should write protocol messages only to stdout. Logs and diagnostics go to stderr.

The command-based config should not publish host port `8080` by default. The proxy only needs the in-container HTTP endpoint, and omitting `-p 8080:8080` avoids conflicts when a user also has a detached DocuMcp container running for the web UI.

### First Tool Call

Before forwarding the first tool call, the proxy runs an `ensureServer` flow:

1. Check whether the in-container HTTP endpoint is already reachable.
2. If reachable, use it.
3. If not reachable, start the normal DocuMcp HTTP server as a child goroutine or child process inside the same container.
4. Wait for the endpoint to become reachable with a short timeout.
5. Forward the tool call to the HTTP MCP endpoint.

If the server cannot start, return a tool error with the startup failure and direct the user to inspect the container logs. The proxy should not try to launch sibling containers or require access to the host container runtime from inside the container.

### Later Tool Calls

After the endpoint is known to be reachable, the proxy forwards calls directly. If a later call fails because the server stopped, it may run `ensureServer` once and retry the call.

When the AI session ends, the command container exits. The named `documcp-data` volume keeps indexed content and stored tokens across sessions.

Users should avoid running the command-based container and the detached web UI container against the same `documcp-data` volume at the same time. If they want the web UI to stay up all day, the URL-based MCP config remains the better fit.

## Configuration

Default values:

```text
HTTP MCP URL: http://127.0.0.1:8080/mcp/http
startup timeout: 20s
data dir: /app/data
```

Suggested flags:

```bash
documcp mcp-proxy \
  --url http://127.0.0.1:8080/mcp/http \
  --startup-timeout 20s
```

The Docker/Podman command line remains responsible for host concerns such as volume mounts, optional port mappings, and environment variables.

## API Key Handling

If `DOCUMCP_API_KEY` is set in the proxy environment, forwarded HTTP MCP requests include:

```text
Authorization: Bearer $DOCUMCP_API_KEY
```

If the server returns `401 unauthorized`, the proxy returns a tool error that says DocuMcp requires `DOCUMCP_API_KEY` in the AI tool environment. It should not print the configured key or attempt to create one.

## Setup Flow

For command-based MCP users, setup is the MCP client config itself. They do not need to clone the repo or install a host binary.

The documentation should include Docker and Podman snippets. A minimal local-only Docker config is:

```json
{
  "mcpServers": {
    "documcp": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-v",
        "documcp-data:/app/data",
        "ghcr.io/mathwro/documcp:latest",
        "mcp-proxy"
      ]
    }
  }
}
```

If users want the web UI to stay running independently of an AI session, they can continue using the existing detached quick start and URL-based MCP config:

```bash
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v documcp-data:/app/data \
  ghcr.io/mathwro/documcp:latest
```

Both modes use the same image and can use the same named volume, but they should not run concurrently against that volume.

## Implementation Notes

The proxy can be implemented in Go as a subcommand of the existing binary. Go is preferred over shell because the proxy must speak MCP over stdio, preserve stdout for JSON-RPC messages, handle structured tool schemas, proxy requests, start the server in-process or as a managed child process, and return clean protocol-level errors.

The container image remains the distribution unit. The first implementation should avoid host runtime orchestration and instead run the proxy and full DocuMcp server inside the same command container.

Recommended package boundaries:

- `cmd/documcp`: parse subcommands and dispatch `serve` versus `mcp-proxy`. Running with no subcommand should preserve the current server behavior.
- `internal/mcpproxy`: stdio MCP server, lazy startup flow, HTTP forwarding.
- `internal/app`: shared server startup wiring that both the default server command and `mcp-proxy` can call.
- `Dockerfile`: switch the runtime stage to `ENTRYPOINT ["documcp"]` so `docker run image mcp-proxy` invokes the subcommand.

If the current MCP library already supports stdio server wiring, reuse it. Otherwise, keep the stdio protocol implementation small and isolated in `internal/mcpproxy`.

## Error Handling

User-facing tool errors should be short and actionable:

- Startup timeout: check the AI tool's MCP server logs or run the Docker/Podman command manually.
- Unauthorized: export `DOCUMCP_API_KEY` in the AI tool environment.
- Endpoint mismatch: pass `--url` if DocuMcp is not listening on the default port.

If Docker or Podman itself is unavailable, the AI client cannot launch the command container and will report that as a command startup problem. The proxy can only turn failures into tool errors after the container process has started.

The docs should separately cover command startup errors:

- Container runtime unavailable: install Docker or Podman.
- Image unavailable: pull `ghcr.io/mathwro/documcp:latest` or check the image name in the MCP config.

## Testing

Unit tests:

- Proxy initialization does not start the full HTTP server.
- `ensureServer` starts the full server on first tool call.
- Startup failure returns a tool-level error.
- `DOCUMCP_API_KEY` is forwarded as an authorization header.
- A stopped server during a later call triggers one lazy retry.

Integration tests:

- Proxy initializes over stdio without loading the model or binding the HTTP port.
- First `list_sources` call starts the in-container server and forwards successfully.
- Unauthorized HTTP response becomes a useful MCP tool error.

Manual verification:

```bash
gofmt -w .
CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags sqlite_fts5 -race ./...
```

## Open Questions

- Should the proxy start the full server in-process as a goroutine, or spawn the same binary as a child process with the normal server mode?
- Should command-based configs publish both Docker and Podman snippets, or choose Docker as the copy-paste default and mention Podman as a substitution?
- Which MCP clients should get first-class config snippets: Claude Desktop, Claude Code, Cursor, Codex, or all of them?

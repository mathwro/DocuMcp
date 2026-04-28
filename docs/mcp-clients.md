# MCP Integration

The MCP server exposes two local transports:

- `http://localhost:8080/mcp/` for Server-Sent Events (SSE) clients such as Claude.
- `http://localhost:8080/mcp` for streamable HTTP clients such as Codex.

## Claude Desktop (`claude_desktop_config.json`)

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

## Claude Code (`.mcp.json` in your project root)

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

Omit the `headers` block when `DOCUMCP_API_KEY` is unset (the typical local-only setup — see [install.md](install.md#when-to-set-documcp_api_key)). Restart Claude Code so it picks up the config change; run `/mcp` to confirm the server is connected.

## Codex (`~/.codex/config.toml`)

```toml
[mcp_servers.documcp]
url = "http://localhost:8080/mcp"
```

If `DOCUMCP_API_KEY` is set, configure Codex with `bearer_token_env_var = "DOCUMCP_API_KEY"` and export the same environment variable before starting Codex.

## Available MCP Tools

| Tool | Description |
|---|---|
| `list_sources` | Lists all configured sources with names, types, page counts, and crawl status |
| `search_docs(query, source?)` | Hybrid search returning up to 10 results with short excerpts (~200 chars) and source names. Use `get_page` for full content. |
| `browse_source(source, section?)` | Hierarchical table of contents — top-level sections with page counts, or up to 50 pages in a section |
| `get_page(url)` | Retrieve the full content of a specific page by URL |

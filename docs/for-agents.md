# For Agents

Use this guide when you are an AI coding assistant helping a user install and configure DocuMcp.

## Goal

Install DocuMcp as a local MCP server, verify that it works, help the user index at least one documentation source, and propose an MCP client configuration for the AI tool currently in use.

## Install

Use Docker or Podman, whichever is available. Start the latest container image with a persistent named volume:

```bash
docker run -d \
  --name documcp \
  -p 8080:8080 \
  -v documcp-data:/app/data \
  ghcr.io/mathwro/documcp:latest
```

If a container named `documcp` already exists, inspect it before changing it. Do not remove containers, volumes, files, or user data without confirmation.

## Verify

Confirm that the web UI is reachable at `http://localhost:8080`.

After the server is running, help the user add at least one documentation source in the web UI and trigger a crawl. Public web docs are the simplest first source.

## Configure MCP

When DocuMcp is reachable and a source has been crawled, generate a proposed MCP config for the AI tool currently in use. Use the MCP endpoint:

```text
http://localhost:8080/mcp
```

Show the proposed config to the user and ask for confirmation before applying it. If the MCP client requires a command-based server entry instead of a remote HTTP endpoint, use [mcp-clients.md](mcp-clients.md).

## Safety

Do not create, store, print, or commit secrets without the user's confirmation. Ask before setting `DOCUMCP_SECRET_KEY`, `DOCUMCP_API_KEY`, source tokens, or local `config.yaml` values.

Do not commit `.env`, real tokens, or local `config.yaml`.

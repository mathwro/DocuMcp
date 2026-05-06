# Troubleshooting

Common symptoms and how to diagnose them. If your issue is not listed, check the container logs first (`docker logs documcp` or `docker compose logs documcp`).

## `/api/*` or `/mcp/*` returns 401 `unauthorized`

`DOCUMCP_API_KEY` is set — every request needs `Authorization: Bearer <key>`. Unset it for local-only use, or pass the header from your MCP client. See [install.md](install.md#when-to-set-documcp_api_key) for guidance on when the key is appropriate.

`config.yaml` does not control this key. Docker/Compose injects `DOCUMCP_API_KEY` into the container environment when the container is created, so removing `config.yaml` will not disable bearer auth.

Check the running container:

```bash
docker compose exec documcp sh -lc 'test -n "$DOCUMCP_API_KEY" && echo DOCUMCP_API_KEY=set || echo DOCUMCP_API_KEY=unset'
```

If it is set and you want local-only unauthenticated access, remove `DOCUMCP_API_KEY` from `.env` or your Compose/service definition, then recreate the container:

```bash
docker compose up -d --force-recreate
```

## The UI loads but `/api/sources` returns 404 in docker-compose

Check `docker compose logs documcp`. The binary inside the container binds to `0.0.0.0:8080` (the image sets `DOCUMCP_BIND_ADDR`); if you changed the port in `config.yaml`, also update the compose `ports:` mapping and — if running outside compose — set `DOCUMCP_BIND_ADDR=0.0.0.0:<new-port>`.

## Stored GitHub / Azure DevOps tokens fail to decrypt after a restart

`DOCUMCP_SECRET_KEY` either changed or was never set. A missing key means DocuMcp generates an ephemeral key per run. Re-auth the source in the UI and set a persistent key. Generate one with `openssl rand -hex 32`.

## Semantic search returns no results but keyword search works

The ONNX embedding model failed to load. Check startup logs for `embedding model not loaded` — usually `DOCUMCP_MODEL_PATH` points to a missing directory. The container image includes the model at `/app/models/all-MiniLM-L6-v2`; for a bare-binary install, export the model yourself with [optimum-cli](https://huggingface.co/docs/optimum/exporters/onnx/usage_guides/export_a_model).

## A source shows 0 pages after a crawl

Check the server logs. Common causes:

- GitHub PAT missing **Contents: Read-only** on the target repo (manifests as `unauthorized — token missing or lacks repo scope`)
- `include_path` with a typo so no file matches
- The web source's sitemap is outside the `include_path` prefix

Trigger **Crawl Now** in the UI and watch the progress badge — it shows `N / Total pages` during a run.

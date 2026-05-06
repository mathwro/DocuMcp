# Security Policy

## Reporting a vulnerability

If you believe you have found a security issue in DocuMcp, please open a
private report via GitHub Security Advisories:

<https://github.com/mathwro/DocuMcp/security/advisories/new>

Please do **not** file a public issue or pull request for security reports.

When reporting, include:

- A description of the issue and its impact
- Steps or a minimal reproducer
- The version or commit SHA you observed it on
- Any mitigations you are already aware of

I aim to acknowledge reports within seven days and to ship a fix or a
coordinated-disclosure plan within thirty days of acknowledgement. I am a
single maintainer — response times are best-effort.

## Supported versions

Security fixes are only backported to the latest tagged release and the
`main` branch. Older tags are not maintained.

## Threat model and non-goals

DocuMcp is designed as a **single-user, local-network** service. The default
bind address is `127.0.0.1:<port>` and the admin UI and MCP endpoints are
intended to be accessed from the same machine.

The following threats are in scope:

- **SSRF** through the web crawler or any source adapter (redirect hops
  re-validated, private/metadata IPs blocked via DNS resolution)
- **XSS** in the Web UI from untrusted source URLs (`safeHref` collapses
  non-http(s) hrefs to `#`, strict CSP)
- **Source URL smuggling** — non-http(s) schemes rejected at the REST
  boundary when creating a source
- **Token exfiltration** via the REST API — `Source.Auth` is marked
  `json:"-"` so stored secrets do not round-trip through `/api/sources`
- **Timing side channels** on the API bearer token comparison
  (`crypto/subtle.ConstantTimeCompare`)
- **Token storage at rest** — PATs and OAuth tokens are encrypted with
  AES-256-GCM using `DOCUMCP_SECRET_KEY`

The following are **not** in scope:

- Multi-tenant hosting or running DocuMcp as an internet-exposed service
  without an authenticating reverse proxy and `DOCUMCP_API_KEY`
- Attacks from someone who already has write access to `config.yaml`
  or the SQLite database on disk — those are treated as trusted inputs
- DoS via crawling very large sources (use `include_path` to scope)
- Supply chain (upstream Go modules, ONNX model, base container image) —
  I trust the standard Go module proxy, Hugging Face, and the Debian
  bookworm-slim base image

## Configuring a hardened deployment

If you do expose DocuMcp beyond a single local user:

1. Set `DOCUMCP_API_KEY` to a strong random value (32+ bytes).
2. Place it behind a TLS-terminating reverse proxy (nginx, Caddy,
   Traefik) — DocuMcp does not serve TLS itself.
3. Keep `DOCUMCP_BIND_ADDR` on a private interface and let the reverse
   proxy reach it over loopback or a private network.
4. Set `DOCUMCP_SECRET_KEY` to a persistent 32-byte value so stored
   tokens survive restarts; back the SQLite data volume up.

`DOCUMCP_API_KEY` and `DOCUMCP_SECRET_KEY` are environment variables, not
`config.yaml` fields. Treat them as deployment secrets and inject them with
Compose, systemd, Kubernetes, or a secret manager.

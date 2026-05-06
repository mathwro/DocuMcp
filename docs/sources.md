# Source Types

DocuMcp supports four source adapters. Each is configured as an entry under `sources:` in `config.yaml`, or added through the Web UI. The source list shows whether each source came from `config.yaml` or the UI. Use **Export YAML** in the Web UI to generate a portable `sources:` block from the current database state.

## Web (`type: web`)

Crawls public websites. Discovers pages via sitemap, falls back to link following. Polite crawling with 500 ms delay between requests and `Retry-After` backoff on HTTP 429.

```yaml
- name: ArgoCD Operator Manual
  type: web
  url: https://argo-cd.readthedocs.io/en/stable/
  include_paths:
    - operator-manual/
    - user-guide/
  crawl_schedule: "@weekly"
```

`include_paths` is a filter: only matching pages are indexed. It does not add extra crawl roots. For web sources, entries can be relative to `url` (`operator-manual/`), root-relative (`/en/stable/operator-manual/`), or full same-origin URLs.

## GitHub Wiki (`type: github_wiki`)

Indexes a GitHub repository's wiki. Public wikis work without authentication. For private wikis, click **Connect** in the Web UI and paste a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) with **Contents: Read-only** on the target repo.

```yaml
- name: My Project Wiki
  type: github_wiki
  repo: owner/repo
  crawl_schedule: "@daily"
```

## GitHub Repo (`type: github_repo`)

Indexes Markdown (`.md`, `.mdx`) and text (`.txt`) files directly from a repository's tree via the GitHub tarball endpoint. Files larger than 5 MiB are skipped. Use `include_paths` to restrict indexing to repository folders such as `docs/` and `help/tests/`.

```yaml
- name: My Project Docs
  type: github_repo
  repo: owner/repo
  branch: main
  include_paths:
    - docs/
    - help/tests/
  crawl_schedule: "@daily"
```

`include_paths` is a filter: only matching files are indexed. It does not add extra repositories or branches.

Public repos work without authentication. For private repos, click **Connect** in the Web UI and paste a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with **Contents: Read-only** on the target repo.

## Azure DevOps Wiki (`type: azure_devops`)

Indexes an Azure DevOps wiki. Authenticates via Microsoft device code flow using the Azure CLI client ID (no admin consent required).

```yaml
- name: Team Wiki
  type: azure_devops
  url: https://dev.azure.com/org/project
  crawl_schedule: "@weekly"
```

> **Shortcut:** Mount `~/.azure` into the container to reuse existing Azure CLI credentials.

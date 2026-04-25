# Source Types

DocuMcp supports four source adapters. Each is configured as an entry under `sources:` in `config.yaml`, or added through the Web UI.

## Web (`type: web`)

Crawls public websites. Discovers pages via sitemap, falls back to link following. Polite crawling with 500 ms delay between requests and `Retry-After` backoff on HTTP 429.

```yaml
- name: ArgoCD Operator Manual
  type: web
  url: https://argo-cd.readthedocs.io/en/stable/
  include_path: https://argo-cd.readthedocs.io/en/stable/operator-manual/
  crawl_schedule: "@weekly"
```

## GitHub Wiki (`type: github_wiki`)

Indexes a GitHub repository's wiki. Public wikis work without authentication. For private wikis, click **Connect** in the Web UI and paste a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) with **Contents: Read-only** on the target repo.

```yaml
- name: My Project Wiki
  type: github_wiki
  repo: owner/repo
  crawl_schedule: "@daily"
```

## GitHub Repo (`type: github_repo`)

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

## Azure DevOps Wiki (`type: azure_devops`)

Indexes an Azure DevOps wiki. Authenticates via Microsoft device code flow using the Azure CLI client ID (no admin consent required).

```yaml
- name: Team Wiki
  type: azure_devops
  url: https://dev.azure.com/org/project
  crawl_schedule: "@weekly"
```

> **Shortcut:** Mount `~/.azure` into the container to reuse existing Azure CLI credentials.

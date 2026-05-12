function blankSource(type = 'web') {
  return { Name: '', Type: type, URL: '', Repo: '', Branch: '', IncludePath: '', IncludePaths: [], CrawlSchedule: '' }
}

function sourceIncludePaths(src) {
  const paths = Array.isArray(src.IncludePaths) ? src.IncludePaths : []
  const combined = [src.IncludePath || '', ...paths]
  return [...new Set(combined.map(p => (p || '').trim()).filter(Boolean))]
}

function prepareSourcePayload(src) {
  const paths = sourceIncludePaths(src)
  return { ...src, IncludePaths: paths, IncludePath: paths[0] || '' }
}

function app() {
  return {
    view: 'sources',
    sources: [],
    showAddForm: false,
    newSource: blankSource(),
    editingSourceId: null,
    editSource: blankSource(),
    deviceCodePending: null,
    pollInterval: null,
    refreshInterval: null,
    tokenPending: null,   // { sourceId, sourceName } when GitHub PAT modal is open
    tokenInput: '',
    tokenError: '',

    // Search state
    searchQuery: '',
    searchResults: [],
    searchLoading: false,
    searchError: '',

    async init() {
      await this.loadSources()
    },

    async loadSources() {
      try {
        const r = await fetch('/api/sources')
        this.sources = await r.json()
        // Keep polling while any source is crawling; stop when all done.
        if (this.sources.some(s => s.Crawling)) {
          this.startRefresh()
        } else {
          this.stopRefresh()
        }
      } catch(e) {
        console.error('loadSources:', e)
      }
    },

    startRefresh() {
      if (this.refreshInterval) return
      this.refreshInterval = setInterval(() => this.loadSources(), 2000)
    },

    stopRefresh() {
      if (this.refreshInterval) {
        clearInterval(this.refreshInterval)
        this.refreshInterval = null
      }
    },

    isCrawling(src) {
      return src.Crawling
    },

    async addSource() {
      const body = prepareSourcePayload(this.newSource)
      try {
        const r = await fetch('/api/sources', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        })
        if (!r.ok) { alert('Failed to add source: ' + await r.text()); return }
        this.showAddForm = false
        this.newSource = blankSource()
        await this.loadSources()
      } catch(e) {
        console.error('addSource:', e)
      }
    },

    startEditSource(src) {
      this.showAddForm = false
      this.editingSourceId = src.ID
      this.editSource = {
        Name: src.Name || '',
        Type: src.Type || 'web',
        URL: src.URL || '',
        Repo: src.Repo || '',
        Branch: src.Branch || '',
        IncludePath: src.IncludePath || '',
        IncludePaths: sourceIncludePaths(src),
        CrawlSchedule: src.CrawlSchedule || ''
      }
    },

    cancelEditSource() {
      this.editingSourceId = null
      this.editSource = blankSource()
    },

    async updateSource(id) {
      const body = prepareSourcePayload(this.editSource)
      try {
        const r = await fetch(`/api/sources/${id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        })
        if (!r.ok) { alert('Failed to update source: ' + await r.text()); return }
        this.cancelEditSource()
        await this.loadSources()
      } catch(e) {
        console.error('updateSource:', e)
      }
    },

    async crawlNow(id) {
      const r = await fetch(`/api/sources/${id}/crawl`, { method: 'POST' })
      if (!r.ok) { alert('Failed to start crawl: ' + await r.text()); return }
      await this.loadSources()
    },

    async stopCrawl(id) {
      const r = await fetch(`/api/sources/${id}/crawl`, { method: 'DELETE' })
      if (!r.ok) { alert('Failed to stop crawl: ' + await r.text()); return }
      await this.loadSources()
    },

    async exportSources() {
      try {
        const r = await fetch('/api/sources/export')
        if (!r.ok) { alert('Failed to export sources: ' + await r.text()); return }
        const blob = await r.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = 'documcp-sources.yaml'
        document.body.appendChild(a)
        a.click()
        a.remove()
        URL.revokeObjectURL(url)
      } catch(e) {
        console.error('exportSources:', e)
      }
    },

    async connectAuth(id, sourceType, sourceName) {
      // GitHub sources use a user-supplied fine-grained PAT.
      if (sourceType === 'github_wiki' || sourceType === 'github_repo') {
        this.tokenPending = { sourceId: id, sourceName: sourceName }
        this.tokenInput = ''
        this.tokenError = ''
        return
      }
      // Azure DevOps uses the Microsoft device-code flow.
      try {
        const r = await fetch(`/api/sources/${id}/auth/start`, { method: 'POST' })
        if (!r.ok) { alert('Failed to start auth: ' + await r.text()); return }
        this.deviceCodePending = { ...await r.json(), sourceId: id }
        this.startPolling(id)
      } catch(e) {
        console.error('connectAuth:', e)
      }
    },

    async saveToken() {
      if (!this.tokenPending) return
      const token = this.tokenInput.trim()
      if (!token) { this.tokenError = 'Token is required.'; return }
      try {
        const r = await fetch(`/api/sources/${this.tokenPending.sourceId}/auth/token`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token })
        })
        if (!r.ok) {
          this.tokenError = 'Failed to save token: ' + await r.text()
          return
        }
        this.tokenPending = null
        this.tokenInput = ''
        this.tokenError = ''
      } catch(e) {
        this.tokenError = e.message
      }
    },

    cancelTokenPrompt() {
      this.tokenPending = null
      this.tokenInput = ''
      this.tokenError = ''
    },

    startPolling(id) {
      if (this.pollInterval) clearInterval(this.pollInterval)
      this.pollInterval = setInterval(async () => {
        try {
          const r = await fetch(`/api/sources/${id}/auth/poll`)
          const data = await r.json()
          if (data.status === 'ok') {
            clearInterval(this.pollInterval)
            this.pollInterval = null
            this.deviceCodePending = null
            await this.loadSources()
          } else if (data.status === 'error') {
            clearInterval(this.pollInterval)
            this.pollInterval = null
            this.deviceCodePending = null
            alert('Auth failed: ' + (data.message || 'unknown error'))
          }
          // status === 'pending' -> keep polling
        } catch(e) {
          console.error('poll:', e)
        }
      }, 5000)
    },

    cancelAuth(id) {
      if (this.pollInterval) clearInterval(this.pollInterval)
      this.pollInterval = null
      this.deviceCodePending = null
      fetch(`/api/sources/${id}/auth`, { method: 'DELETE' }).catch(() => {})
    },

    async deleteSource(id) {
      if (!confirm('Remove this source and all its indexed pages?')) return
      try {
        const r = await fetch(`/api/sources/${id}`, { method: 'DELETE' })
        if (!r.ok) { alert('Failed to delete: ' + await r.text()); return }
        await this.loadSources()
      } catch(e) {
        console.error('deleteSource:', e)
      }
    },

    async doSearch() {
      if (!this.searchQuery.trim()) return
      this.searchLoading = true
      this.searchError = ''
      this.searchResults = []
      try {
        const r = await fetch('/api/search?q=' + encodeURIComponent(this.searchQuery))
        if (!r.ok) { this.searchError = await r.text(); return }
        this.searchResults = await r.json()
      } catch(e) {
        this.searchError = e.message
      } finally {
        this.searchLoading = false
      }
    },

    // safeHref returns u only if it is a plain http(s) URL. Anything else
    // (javascript:, data:, file:, etc.) collapses to "#" so a malicious
    // source URL cannot execute script via the href binding.
    safeHref(u) {
      if (typeof u !== 'string') return '#'
      const s = u.trim()
      if (/^https?:\/\//i.test(s)) return s
      return '#'
    },

    resultTitle(r) {
      if (r.Title) return r.Title
      try {
        const seg = new URL(r.URL).pathname.replace(/\/$/, '').split('/').filter(Boolean).pop()
        return seg ? seg.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) : r.URL
      } catch { return r.URL }
    },

    sourceTypeName(type) {
      const map = { web: 'Web', github_wiki: 'GitHub Wiki', github_repo: 'GitHub Repo', azure_devops: 'Azure DevOps' }
      return map[type] || type
    },

    sourceOriginName(origin) {
      return origin === 'config' ? 'Config' : 'UI'
    },

    includePathPlaceholder(type) {
      return type === 'github_repo' ? 'docs/' : '/guides/'
    },

    includePathHelp(type) {
      if (type === 'github_repo') return 'Only files under these repository folders will be indexed. Leave empty to index the whole repo.'
      return 'Only same-site URLs under these path prefixes will be indexed. Use paths like /guides/ or guides/; full same-site URLs also work. Leave empty to use the source URL path.'
    },

    ensureIncludePathRow(src) {
      if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
      if (src.IncludePaths.length === 0) src.IncludePaths.push('')
    },

    addIncludePathRow(src) {
      if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
      src.IncludePaths.push('')
    },

    removeIncludePathRow(src, index) {
      if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
      src.IncludePaths.splice(index, 1)
    },

    sourceDisplayPaths(src) {
      return sourceIncludePaths(src).join(', ')
    },

    sourceDisplayURL(src) {
      if (!src) return ''
      if (src.Type === 'github_wiki' && src.Repo) return `https://github.com/${src.Repo}/wiki`
      if (src.Type === 'github_repo' && src.Repo) {
        const branch = src.Branch || 'main'
        return `https://github.com/${src.Repo}/tree/${branch}`
      }
      return src.URL || src.BaseURL || ''
    },

    formatDate(ts) {
      if (!ts) return ''
      return new Date(ts).toLocaleString()
    }
  }
}

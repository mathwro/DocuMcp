function app() {
  return {
    view: 'sources',
    sources: [],
    showAddForm: false,
    newSource: { Name: '', Type: 'web', URL: '', Repo: '', IncludePath: '', CrawlSchedule: '' },
    deviceCodePending: null,
    pollInterval: null,
    refreshInterval: null,

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
      const body = { ...this.newSource }
      try {
        const r = await fetch('/api/sources', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        })
        if (!r.ok) { alert('Failed to add source: ' + await r.text()); return }
        this.showAddForm = false
        this.newSource = { Name: '', Type: 'web', URL: '', Repo: '', IncludePath: '', CrawlSchedule: '' }
        await this.loadSources()
      } catch(e) {
        console.error('addSource:', e)
      }
    },

    async crawlNow(id) {
      await fetch(`/api/sources/${id}/crawl`, { method: 'POST' })
      await this.loadSources()
    },

    async connectAuth(id) {
      try {
        const r = await fetch(`/api/sources/${id}/auth/start`, { method: 'POST' })
        if (!r.ok) { alert('Failed to start auth: ' + await r.text()); return }
        this.deviceCodePending = { ...await r.json(), sourceId: id }
        this.startPolling(id)
      } catch(e) {
        console.error('connectAuth:', e)
      }
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
        this.crawlingIds.delete(id)
        if (this.crawlingIds.size === 0) this.stopRefresh()
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

    resultTitle(r) {
      if (r.Title) return r.Title
      try {
        const seg = new URL(r.URL).pathname.replace(/\/$/, '').split('/').filter(Boolean).pop()
        return seg ? seg.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) : r.URL
      } catch { return r.URL }
    },

    sourceTypeName(type) {
      const map = { web: 'Web', github_wiki: 'GitHub Wiki', azure_devops: 'Azure DevOps' }
      return map[type] || type
    },

    formatDate(ts) {
      if (!ts) return ''
      return new Date(ts).toLocaleString()
    }
  }
}

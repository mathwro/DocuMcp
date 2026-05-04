# Lazy MCP Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an image-based command MCP mode so users can configure AI tools with `docker run ... ghcr.io/mathwro/documcp:latest mcp-proxy`, avoid startup errors when the full server is idle, and keep the existing container-first install story.

**Architecture:** Refactor the current server startup into an internal app package that can be started by the default command or lazily by the proxy. Add a `mcp-proxy` subcommand that runs an MCP stdio server immediately, starts the normal HTTP server only on first tool call, and forwards tool calls to the in-container streamable HTTP endpoint. Change the Docker image to use `ENTRYPOINT ["documcp"]` so image arguments become subcommands while running the image with no arguments still starts the normal server.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk/mcp`, `net/http`, SQLite with `sqlite_fts5`, Docker/Podman runtime image.

---

## File Map

- Modify `cmd/documcp/main.go`: turn the current single-mode `main` into command dispatch for default server mode and `mcp-proxy`.
- Modify `cmd/documcp/main_test.go`: keep existing bind address coverage and add subcommand parsing coverage.
- Create `internal/app/app.go`: own full DocuMcp HTTP server construction, start, shutdown, config loading, bind address selection, model loading, scheduler setup, and token key derivation.
- Create `internal/app/app_test.go`: verify config defaults, bind address behavior, startup on an ephemeral port, and graceful shutdown.
- Modify `internal/mcp/server.go`: expose reusable tool definitions and a registration helper so the proxy can advertise the same tools without duplicating schemas.
- Modify `internal/mcp/tools.go`: move tool metadata into exported definition helpers while leaving existing handlers unchanged.
- Modify `internal/mcp/server_test.go`: verify HTTP MCP server still registers the four expected tools after the refactor.
- Create `internal/mcpproxy/proxy.go`: implement lazy stdio MCP server, `ensureServer`, HTTP MCP forwarding, retry, and shutdown coordination.
- Create `internal/mcpproxy/proxy_test.go`: unit-test lazy startup, auth header forwarding, startup errors, and retry behavior using fakes.
- Modify `Dockerfile`: switch runtime image from `CMD ["documcp"]` to `ENTRYPOINT ["documcp"]` plus default `CMD []`.
- Modify `docs/mcp-clients.md`: add Docker and Podman command-based MCP config snippets.
- Modify `docs/for-agents.md`: tell agents to choose command-based config when the client prefers stdio command servers.
- Modify `docs/install.md`: explain detached web UI mode versus command-container mode and warn against concurrent use of the same volume.

## Task 1: Extract App Startup Helpers

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`
- Modify: `cmd/documcp/main.go`
- Modify: `cmd/documcp/main_test.go`

- [ ] **Step 1: Write failing helper tests**

Create `internal/app/app_test.go` with these tests:

```go
package app

import (
	"os"
	"testing"
)

func TestBindAddrDefaultsToLoopback(t *testing.T) {
	t.Setenv("DOCUMCP_BIND_ADDR", "")
	got := BindAddr(8080)
	if got != "127.0.0.1:8080" {
		t.Fatalf("BindAddr(8080) = %q, want 127.0.0.1:8080", got)
	}
}

func TestBindAddrUsesEnvOverride(t *testing.T) {
	t.Setenv("DOCUMCP_BIND_ADDR", "0.0.0.0:9090")
	got := BindAddr(8080)
	if got != "0.0.0.0:9090" {
		t.Fatalf("BindAddr with override = %q, want 0.0.0.0:9090", got)
	}
}

func TestConfigPathDefaultsToContainerPath(t *testing.T) {
	if err := os.Unsetenv("DOCUMCP_CONFIG"); err != nil {
		t.Fatalf("Unsetenv DOCUMCP_CONFIG: %v", err)
	}
	path, explicit := ConfigPathFromEnv()
	if explicit {
		t.Fatalf("ConfigPathFromEnv explicit = true, want false")
	}
	if path != "/app/config.yaml" {
		t.Fatalf("ConfigPathFromEnv path = %q, want /app/config.yaml", path)
	}
}

func TestConfigPathUsesExplicitEnv(t *testing.T) {
	t.Setenv("DOCUMCP_CONFIG", "/tmp/documcp/config.yaml")
	path, explicit := ConfigPathFromEnv()
	if !explicit {
		t.Fatalf("ConfigPathFromEnv explicit = false, want true")
	}
	if path != "/tmp/documcp/config.yaml" {
		t.Fatalf("ConfigPathFromEnv path = %q, want /tmp/documcp/config.yaml", path)
	}
}
```

Move the existing bind address tests out of `cmd/documcp/main_test.go` or update them to call `app.BindAddr`.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/app ./cmd/documcp
```

Expected: fail because `internal/app` and `BindAddr`/`ConfigPathFromEnv` do not exist.

- [ ] **Step 3: Add minimal helper implementation**

Create `internal/app/app.go` with:

```go
package app

import (
	"fmt"
	"os"
)

func ConfigPathFromEnv() (string, bool) {
	cfgPath, explicit := os.LookupEnv("DOCUMCP_CONFIG")
	if !explicit {
		return "/app/config.yaml", false
	}
	return cfgPath, true
}

func BindAddr(port int) string {
	if v := os.Getenv("DOCUMCP_BIND_ADDR"); v != "" {
		return v
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

Update `cmd/documcp/main.go` to call `app.ConfigPathFromEnv()` and `app.BindAddr()` but leave the rest of the server wiring in place for this task.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/app ./cmd/documcp
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go cmd/documcp/main.go cmd/documcp/main_test.go
git commit -m "refactor(app): extract startup helpers"
```

## Task 2: Move Full Server Lifecycle Into `internal/app`

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `cmd/documcp/main.go`

- [ ] **Step 1: Write failing lifecycle test**

Append this test to `internal/app/app_test.go`:

```go
func TestAppStartsAndShutsDown(t *testing.T) {
	t.Setenv("DOCUMCP_CONFIG", "")
	t.Setenv("DOCUMCP_BIND_ADDR", "127.0.0.1:0")
	t.Setenv("DOCUMCP_MODEL_PATH", t.TempDir()+"/missing-model")
	t.Setenv("DOCUMCP_SECRET_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	dataDir := t.TempDir()
	a, err := New(Options{
		Config: &config.Config{
			Server: config.ServerConfig{
				Port:    8080,
				DataDir: dataDir,
			},
		},
	})
	if err != nil {
		t.Fatalf("New app: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start app: %v", err)
	}
	if a.Addr() == "" {
		t.Fatalf("Addr() is empty after Start")
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown app: %v", err)
	}
}
```

Add imports for `context` and `github.com/mathwro/DocuMcp/internal/config`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/app
```

Expected: fail because `Options`, `New`, `Start`, `Addr`, and `Shutdown` do not exist.

- [ ] **Step 3: Implement app lifecycle**

Expand `internal/app/app.go` with these public types and methods:

```go
type Options struct {
	Config     *config.Config
	ConfigPath string
	ExplicitConfig bool
}

type App struct {
	store     *db.Store
	scheduler *crawler.Scheduler
	apiServer *api.Server
	httpServer *http.Server
	listener  net.Listener
}

func New(opts Options) (*App, error) {
	cfg := opts.Config
	if cfg == nil {
		var err error
		cfg, err = loadConfig(opts.ConfigPath, opts.ExplicitConfig)
		if err != nil {
			return nil, err
		}
	}

	store, err := db.Open(cfg.Server.DataDir + "/documcp.db")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	modelPath := getenv("DOCUMCP_MODEL_PATH", "/app/models/all-MiniLM-L6-v2")
	var embedder *embed.Embedder
	if _, statErr := os.Stat(modelPath); statErr == nil {
		embedder, err = embed.New(modelPath)
		if err != nil {
			slog.Warn("embedding model not loaded, semantic search disabled", "err", err)
		} else {
			slog.Info("embedding model loaded", "path", modelPath)
		}
	}

	key := deriveKey()
	tokenStore := auth.NewTokenStore(store, key)
	c := crawler.New(store, embedder).WithTokenStore(tokenStore)
	scheduler := crawler.NewScheduler(c, store)
	scheduler.Load(cfg)

	mcpServer := mcp.NewServer(store, embedder)
	apiServer := api.NewServerWithMCPHandlers(store, c, mcpServer.Handler(), mcpServer.StreamableHTTPHandler(), key)

	return &App{
		store:     store,
		scheduler: scheduler,
		apiServer: apiServer,
		httpServer: &http.Server{
			Addr:         BindAddr(cfg.Server.Port),
			Handler:      apiServer,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
	}, nil
}

func (a *App) Start() error {
	ln, err := net.Listen("tcp", a.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	a.listener = ln
	go func() {
		if err := a.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
		}
	}()
	return nil
}

func (a *App) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *App) Shutdown(ctx context.Context) error {
	a.apiServer.Shutdown()
	err := a.httpServer.Shutdown(ctx)
	drainCtx := a.scheduler.Stop()
	<-drainCtx.Done()
	if a.store != nil {
		a.store.Close()
	}
	return err
}
```

Move `deriveKey` from `cmd/documcp/main.go` into `internal/app/app.go`. Keep its behavior unchanged.

Add this private loader:

```go
func loadConfig(path string, explicit bool) (*config.Config, error) {
	if path == "" {
		path, explicit = ConfigPathFromEnv()
	}
	loader := config.LoadOrDefault
	if explicit {
		loader = config.Load
	}
	cfg, err := loader(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}
```

Update `cmd/documcp/main.go` so server mode creates `app.New(app.Options{})`, calls `Start`, waits for SIGINT/SIGTERM, and calls `Shutdown`.

- [ ] **Step 4: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/app ./cmd/documcp
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go cmd/documcp/main.go
git commit -m "refactor(app): share server lifecycle"
```

## Task 3: Add Subcommand Dispatch

**Files:**
- Modify: `cmd/documcp/main.go`
- Modify: `cmd/documcp/main_test.go`

- [ ] **Step 1: Write failing command parsing tests**

Add to `cmd/documcp/main_test.go`:

```go
func TestCommandMode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want commandMode
	}{
		{name: "no args starts server", args: nil, want: commandServe},
		{name: "serve starts server", args: []string{"serve"}, want: commandServe},
		{name: "mcp proxy", args: []string{"mcp-proxy"}, want: commandMCPProxy},
		{name: "unknown", args: []string{"nope"}, want: commandUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseCommand(test.args); got != test.want {
				t.Fatalf("parseCommand(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./cmd/documcp
```

Expected: fail because `commandMode` and `parseCommand` do not exist.

- [ ] **Step 3: Implement dispatch**

Add to `cmd/documcp/main.go`:

```go
type commandMode int

const (
	commandServe commandMode = iota
	commandMCPProxy
	commandUnknown
)

func parseCommand(args []string) commandMode {
	if len(args) == 0 {
		return commandServe
	}
	switch args[0] {
	case "serve":
		return commandServe
	case "mcp-proxy":
		return commandMCPProxy
	default:
		return commandUnknown
	}
}
```

Change `main` to:

```go
func main() {
	switch parseCommand(os.Args[1:]) {
	case commandServe:
		runServe()
	case commandMCPProxy:
		runMCPProxy()
	default:
		fmt.Fprintf(os.Stderr, "usage: documcp [serve|mcp-proxy]\n")
		os.Exit(2)
	}
}
```

For this task, make `runMCPProxy` return a clear intermediate-build error so dispatch compiles:

```go
func runMCPProxy() {
	fmt.Fprintln(os.Stderr, "mcp-proxy support is unavailable in this intermediate build")
	os.Exit(2)
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./cmd/documcp
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/documcp/main.go cmd/documcp/main_test.go
git commit -m "feat(cli): add command dispatch"
```

## Task 4: Reuse MCP Tool Definitions

**Files:**
- Modify: `internal/mcp/tools.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/server_test.go`

- [ ] **Step 1: Write failing tool definition test**

Add to `internal/mcp/server_test.go`:

```go
func TestToolDefinitionsIncludeExpectedTools(t *testing.T) {
	defs := ToolDefinitions()
	got := make(map[string]bool, len(defs))
	for _, def := range defs {
		got[def.Tool.Name] = true
		if len(def.Tool.InputSchema) == 0 {
			t.Fatalf("tool %s has empty input schema", def.Tool.Name)
		}
	}
	for _, name := range []string{"list_sources", "search_docs", "browse_source", "get_page"} {
		if !got[name] {
			t.Fatalf("ToolDefinitions missing %s", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcp
```

Expected: fail because `ToolDefinitions` does not exist.

- [ ] **Step 3: Extract definitions**

In `internal/mcp/tools.go`, add:

```go
type ToolDefinition struct {
	Tool *sdkmcp.Tool
}

func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{Tool: listSourcesTool()},
		{Tool: searchDocsTool()},
		{Tool: browseSourceTool()},
		{Tool: getPageTool()},
	}
}
```

Move each existing inline `&sdkmcp.Tool{...}` literal into functions:

```go
func listSourcesTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name: "list_sources",
		Description: "List all configured documentation sources with their names, types, URLs, " +
			"page counts, and last crawl times. Call this first if you do not know what sources " +
			"are available. Source names are required parameters for search_docs and browse_source.",
		InputSchema: objectSchema,
	}
}
```

Repeat for `searchDocsTool`, `browseSourceTool`, and `getPageTool` by moving the exact existing descriptions and schemas without changing text.

Update `registerTools` to call the helper functions:

```go
func (s *Server) registerTools() {
	s.server.AddTool(listSourcesTool(), s.handleListSources)
	s.server.AddTool(searchDocsTool(), s.handleSearchDocs)
	s.server.AddTool(browseSourceTool(), s.handleBrowseSource)
	s.server.AddTool(getPageTool(), s.handleGetPage)
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcp
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server.go internal/mcp/server_test.go
git commit -m "refactor(mcp): expose tool definitions"
```

## Task 5: Implement Lazy Proxy Server Skeleton

**Files:**
- Create: `internal/mcpproxy/proxy.go`
- Create: `internal/mcpproxy/proxy_test.go`
- Modify: `cmd/documcp/main.go`

- [ ] **Step 1: Write failing lazy-start test**

Create `internal/mcpproxy/proxy_test.go`:

```go
package mcpproxy

import (
	"context"
	"testing"
)

func TestEnsureServerStartsOnlyOnce(t *testing.T) {
	starts := 0
	p := New(Options{
		Endpoint: "http://127.0.0.1:8080/mcp/http",
		StartServer: func(context.Context) error {
			starts++
			return nil
		},
		Ready: func(context.Context, string) error {
			return nil
		},
	})

	if starts != 0 {
		t.Fatalf("server started during New, starts=%d", starts)
	}
	if err := p.ensureServer(context.Background()); err != nil {
		t.Fatalf("first ensureServer: %v", err)
	}
	if err := p.ensureServer(context.Background()); err != nil {
		t.Fatalf("second ensureServer: %v", err)
	}
	if starts != 1 {
		t.Fatalf("starts=%d, want 1", starts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy
```

Expected: fail because `internal/mcpproxy` does not exist.

- [ ] **Step 3: Implement skeleton**

Create `internal/mcpproxy/proxy.go`:

```go
package mcpproxy

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Options struct {
	Endpoint       string
	StartupTimeout time.Duration
	StartServer    func(context.Context) error
	Ready          func(context.Context, string) error
	Shutdown       func(context.Context) error
}

type Proxy struct {
	endpoint       string
	startupTimeout time.Duration
	startServer    func(context.Context) error
	ready          func(context.Context, string) error
	shutdown       func(context.Context) error
	mu             sync.Mutex
	started        bool
}

func New(opts Options) *Proxy {
	timeout := opts.StartupTimeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	return &Proxy{
		endpoint:       opts.Endpoint,
		startupTimeout: timeout,
		startServer:    opts.StartServer,
		ready:          opts.Ready,
		shutdown:       opts.Shutdown,
	}
}

func (p *Proxy) ensureServer(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return nil
	}
	if p.startServer == nil {
		return fmt.Errorf("start server callback is not configured")
	}
	if p.ready == nil {
		return fmt.Errorf("ready check callback is not configured")
	}
	startCtx, cancel := context.WithTimeout(ctx, p.startupTimeout)
	defer cancel()
	if err := p.startServer(startCtx); err != nil {
		return fmt.Errorf("start DocuMcp server: %w", err)
	}
	if err := p.ready(startCtx, p.endpoint); err != nil {
		return fmt.Errorf("wait for DocuMcp server: %w", err)
	}
	p.started = true
	return nil
}
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpproxy/proxy.go internal/mcpproxy/proxy_test.go
git commit -m "feat(mcpproxy): add lazy startup skeleton"
```

## Task 6: Start App Lazily From Proxy

**Files:**
- Modify: `internal/mcpproxy/proxy.go`
- Modify: `internal/mcpproxy/proxy_test.go`
- Modify: `cmd/documcp/main.go`

- [ ] **Step 1: Write failing ready-check test**

Add to `internal/mcpproxy/proxy_test.go`:

```go
func TestReadyCheckReturnsErrorForUnreachableEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := WaitHTTPReady(ctx, "http://127.0.0.1:1/mcp/http")
	if err == nil {
		t.Fatalf("WaitHTTPReady returned nil for unreachable endpoint")
	}
}
```

Add `time` to imports.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy
```

Expected: fail because `WaitHTTPReady` does not exist.

- [ ] **Step 3: Implement app-backed proxy constructor and ready check**

Add to `internal/mcpproxy/proxy.go`:

```go
func WaitHTTPReady(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
```

Add a constructor used by the CLI:

```go
func NewAppProxy(endpoint string) *Proxy {
	var running *app.App
	return New(Options{
		Endpoint: endpoint,
		StartServer: func(ctx context.Context) error {
			a, err := app.New(app.Options{})
			if err != nil {
				return err
			}
			if err := a.Start(); err != nil {
				return err
			}
			running = a
			return nil
		},
		Ready: WaitHTTPReady,
		Shutdown: func(ctx context.Context) error {
			if running == nil {
				return nil
			}
			return running.Shutdown(ctx)
		},
	})
}
```

- [ ] **Step 4: Wire CLI to construct proxy**

In `cmd/documcp/main.go`, replace the temporary `runMCPProxy` body with:

```go
func runMCPProxy() {
	const endpoint = "http://127.0.0.1:8080/mcp/http"
	p := mcpproxy.NewAppProxy(endpoint)
	if err := p.Run(context.Background()); err != nil {
		slog.Error("mcp proxy", "err", err)
		os.Exit(1)
	}
}
```

Add this temporary `Run` implementation so the package compiles before stdio forwarding is added:

```go
func (p *Proxy) Run(ctx context.Context) error {
	<-ctx.Done()
	if p.shutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return p.shutdown(shutdownCtx)
	}
	return ctx.Err()
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy ./cmd/documcp
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mcpproxy/proxy.go internal/mcpproxy/proxy_test.go cmd/documcp/main.go
git commit -m "feat(mcpproxy): start app lazily"
```

## Task 7: Implement Stdio MCP Proxy and HTTP Forwarding

**Files:**
- Modify: `internal/mcpproxy/proxy.go`
- Modify: `internal/mcpproxy/proxy_test.go`
- Modify: `internal/mcp/tools.go`

- [ ] **Step 1: Write failing auth header transport test**

Add to `internal/mcpproxy/proxy_test.go`:

```go
func TestBearerRoundTripperAddsAuthorization(t *testing.T) {
	var got string
	rt := bearerRoundTripper{
		token: "secret",
		next: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			got = req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp/http", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
```

Add imports for `io`, `net/http`, `net/http/httptest`, and `strings`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy
```

Expected: fail because `bearerRoundTripper` does not exist.

- [ ] **Step 3: Implement stdio server**

Use the official Go SDK stdio API:

```go
server := sdkmcp.NewServer(&sdkmcp.Implementation{
	Name:    "DocuMcp Proxy",
	Version: "1.0.0",
}, nil)
if err := server.Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
	return err
}
```

Implement `Run` so it registers proxy handlers for each tool definition:

```go
func (p *Proxy) Run(ctx context.Context) error {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "DocuMcp Proxy",
		Version: "1.0.0",
	}, nil)
	for _, def := range documcp.ToolDefinitions() {
		tool := def.Tool
		server.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return p.callTool(ctx, req)
		})
	}
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}
```

When adding this code, import the existing MCP package with an alias to avoid a name collision:

```go
documcp "github.com/mathwro/DocuMcp/internal/mcp"
sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
```

- [ ] **Step 4: Implement bearer transport and forwarding**

Add:

```go
type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if b.token != "" {
		clone.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.next.RoundTrip(clone)
}
```

Implement `callTool` using the SDK streamable HTTP client:

```go
func (p *Proxy) callTool(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if err := p.ensureServer(ctx); err != nil {
		return toolError("DocuMcp is not ready: " + err.Error()), nil
	}
	session, err := p.connect(ctx)
	if err != nil {
		if retryErr := p.resetAndEnsure(ctx); retryErr == nil {
			session, err = p.connect(ctx)
		}
	}
	if err != nil {
		return toolError("connect to DocuMcp: " + err.Error()), nil
	}
	defer session.Close()
	return session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      req.Params.Name,
		Arguments: req.Params.Arguments,
	})
}
```

Implement `connect`:

```go
func (p *Proxy) connect(ctx context.Context) (*sdkmcp.ClientSession, error) {
	rt := http.DefaultTransport
	if token := os.Getenv("DOCUMCP_API_KEY"); token != "" {
		rt = bearerRoundTripper{token: token, next: rt}
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "DocuMcp Proxy Client", Version: "1.0.0"}, nil)
	return client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint: p.endpoint,
		HTTPClient: &http.Client{
			Transport: rt,
		},
	}, nil)
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/mcpproxy ./internal/mcp
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mcpproxy/proxy.go internal/mcpproxy/proxy_test.go internal/mcp/tools.go
git commit -m "feat(mcpproxy): proxy stdio tool calls"
```

## Task 8: Update Docker Image Entrypoint

**Files:**
- Modify: `Dockerfile`
- Modify: `docs/install.md`
- Modify: `docs/mcp-clients.md`

- [ ] **Step 1: Write down expected Docker behavior**

Before editing, record these expected commands in the PR notes or local test log:

```bash
docker run --rm ghcr.io/mathwro/documcp:latest
docker run --rm ghcr.io/mathwro/documcp:latest serve
docker run --rm -i ghcr.io/mathwro/documcp:latest mcp-proxy
```

Expected after the change:

- No extra args starts the normal server.
- `serve` starts the normal server.
- `mcp-proxy` starts the stdio proxy.

- [ ] **Step 2: Modify Dockerfile**

Replace:

```dockerfile
CMD ["documcp"]
```

With:

```dockerfile
ENTRYPOINT ["documcp"]
CMD []
```

- [ ] **Step 3: Update docs**

In `docs/mcp-clients.md`, add a command-based section with this Docker config:

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

Add a Podman equivalent by replacing `"docker"` with `"podman"`.

In `docs/install.md`, add a short note:

```markdown
Command-based MCP clients can run the image only when an AI session needs DocuMcp. This mode uses the same `documcp-data` named volume, but do not run it concurrently with a detached DocuMcp container against the same volume.
```

- [ ] **Step 4: Build local image**

Run:

```bash
make docker
```

Expected: `documcp:local` builds successfully.

- [ ] **Step 5: Manually verify entrypoint**

Run:

```bash
docker run --rm documcp:local --help
```

Expected: prints usage for `documcp [serve|mcp-proxy]` and exits with status 2.

Run:

```bash
docker run --rm -i documcp:local mcp-proxy
```

Expected: process waits for MCP stdio input without printing non-protocol output to stdout. Stop it with Ctrl-C.

- [ ] **Step 6: Commit**

```bash
git add Dockerfile docs/install.md docs/mcp-clients.md
git commit -m "feat(docker): support image subcommands"
```

## Task 9: Add Agent Setup Guidance

**Files:**
- Modify: `docs/for-agents.md`
- Modify: `docs/mcp-clients.md`
- Modify: `docs/troubleshooting.md`

- [ ] **Step 1: Update agent guide**

In `docs/for-agents.md`, replace the command-based fallback sentence with:

```markdown
If the MCP client requires a command-based stdio server, configure it to run the DocuMcp image with `mcp-proxy` instead of asking the user to clone this repository. Use [mcp-clients.md](mcp-clients.md) for Docker and Podman snippets.
```

- [ ] **Step 2: Add troubleshooting entries**

In `docs/troubleshooting.md`, add:

```markdown
## Command-based MCP config cannot find `docker` or `podman`

The AI tool launches the configured command directly. Install Docker or Podman, or change the MCP config command to the runtime you use.

## Command-based MCP config cannot pull the image

Run `docker pull ghcr.io/mathwro/documcp:latest` or `podman pull ghcr.io/mathwro/documcp:latest` manually to see the registry error. If you pin a version tag, make sure the tag exists.

## Command-based MCP config starts but tools fail

Run the configured `docker run --rm -i ... mcp-proxy` command manually and check stderr. If `DOCUMCP_API_KEY` is set, pass the same environment variable through the container with `-e DOCUMCP_API_KEY`.
```

- [ ] **Step 3: Run markdown sanity search**

Run:

```bash
rg -n "mcp-proxy|command-based|documcp-data" docs README.md
```

Expected: snippets appear in `docs/mcp-clients.md`, `docs/for-agents.md`, `docs/install.md`, and `docs/troubleshooting.md`.

- [ ] **Step 4: Commit**

```bash
git add docs/for-agents.md docs/mcp-clients.md docs/troubleshooting.md
git commit -m "docs: add command-based MCP setup"
```

## Task 10: Final Verification

**Files:**
- No new files; verify full branch.

- [ ] **Step 1: Format Go code**

Run:

```bash
gofmt -w cmd internal
```

Expected: no output.

- [ ] **Step 2: Run vet**

Run:

```bash
CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...
```

Expected: pass.

- [ ] **Step 3: Run race tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 -race ./...
```

Expected: pass.

- [ ] **Step 4: Build binary**

Run:

```bash
make build
```

Expected: `bin/documcp` is created.

- [ ] **Step 5: Build container**

Run:

```bash
make docker
```

Expected: `documcp:local` builds successfully.

- [ ] **Step 6: Verify Docker command mode**

Run:

```bash
docker run --rm -i -v documcp-data:/app/data documcp:local mcp-proxy
```

Expected: the process stays running and does not print logs to stdout before receiving MCP input. Stop it with Ctrl-C.

- [ ] **Step 7: Verify detached mode still works**

Run:

```bash
docker run --rm -p 8080:8080 -v documcp-data:/app/data documcp:local
```

Expected: server starts and logs `starting DocuMcp` on stderr/stdout. Stop it with Ctrl-C.

- [ ] **Step 8: Commit any final fixes**

```bash
git status --short
git add .
git commit -m "test: verify lazy MCP proxy"
```

Only make this commit if verification required additional file changes.

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mathwro/DocuMcp/internal/api"
	"github.com/mathwro/DocuMcp/internal/auth"
	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/crawler"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/embed"
	"github.com/mathwro/DocuMcp/internal/mcp"
)

func main() {
	// If DOCUMCP_CONFIG is set, the user explicitly chose a path — fail loudly if
	// it cannot be read so typos are caught. If it is unset, fall back to defaults
	// when /app/config.yaml is absent so a fresh container needs no bind mount.
	cfgPath, explicit := os.LookupEnv("DOCUMCP_CONFIG")
	if !explicit {
		cfgPath = "/app/config.yaml"
	}
	loader := config.LoadOrDefault
	if explicit {
		loader = config.Load
	}
	cfg, err := loader(cfgPath)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	store, err := db.Open(cfg.Server.DataDir + "/documcp.db")
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	modelPath := getenv("DOCUMCP_MODEL_PATH", "/app/models/all-MiniLM-L6-v2")
	var embedder *embed.Embedder
	if _, statErr := os.Stat(modelPath); statErr == nil {
		embedder, err = embed.New(modelPath)
		if err != nil {
			slog.Warn("embedding model not loaded, semantic search disabled", "err", err)
		} else {
			defer embedder.Close()
			slog.Info("embedding model loaded", "path", modelPath)
		}
	}

	// Derive the encryption key and create the shared token store.
	// The same key is used by both the API server (for OAuth flows) and the
	// crawler (to load stored tokens at crawl time).
	key := deriveKey()
	tokenStore := auth.NewTokenStore(store, key)

	c := crawler.New(store, embedder).WithTokenStore(tokenStore)
	scheduler := crawler.NewScheduler(c, store)
	scheduler.Load(cfg)

	// Reload config on file change. Non-fatal if watch cannot be established.
	watcher, err := config.Watch(cfgPath, func(newCfg *config.Config) {
		slog.Info("config reloaded")
		scheduler.Load(newCfg)
	})
	if err != nil {
		slog.Warn("config watcher not started", "err", err)
	} else {
		defer watcher.Stop()
	}

	mcpServer := mcp.NewServer(store, embedder)
	apiServer := api.NewServerWithMCPHandlers(store, c, mcpServer.Handler(), mcpServer.StreamableHTTPHandler(), key)

	// Log whether API/MCP bearer-token auth is enabled.
	api.LogAPIKeyStatus()

	addr := bindAddr(cfg.Server.Port)
	slog.Info("starting DocuMcp", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      apiServer,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	signal.Stop(quit) // deregister so a second SIGINT uses the default handler
	slog.Info("shutting down")

	// Cancel background crawls started via the API.
	apiServer.Shutdown()

	// Give active HTTP connections up to 30 s to finish.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}

	drainCtx := scheduler.Stop()
	<-drainCtx.Done()
}

// deriveKey returns the 32-byte AES-256-GCM key for token encryption.
// It reads DOCUMCP_SECRET_KEY (hex or base64-encoded 32 bytes). If the env
// var is absent or invalid, a random ephemeral key is generated and a warning
// is logged — tokens will not survive process restarts in that case.
//
// If the key changes between restarts, any tokens encrypted with the old key
// will fail to decrypt. The crawler logs a clear error in that case and
// proceeds unauthenticated; the user should re-authenticate via the UI.
func deriveKey() []byte {
	raw := os.Getenv("DOCUMCP_SECRET_KEY")
	if raw != "" {
		// Try hex first (64 hex chars = 32 bytes).
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b
		}
		// Fall back to standard base64 (44 base64 chars = 32 bytes).
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
			return b
		}
		// Fall back to URL-safe base64 (produced by openssl, Python urlsafe_b64encode, etc.).
		if b, err := base64.URLEncoding.DecodeString(raw); err == nil && len(b) == 32 {
			return b
		}
		slog.Warn("DOCUMCP_SECRET_KEY is set but could not be decoded as 32-byte hex or base64; using ephemeral key")
	} else {
		slog.Warn("DOCUMCP_SECRET_KEY not set; using ephemeral key — stored tokens will not survive restarts")
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// rand.Read cannot fail on supported platforms, but handle defensively.
		slog.Error("generate ephemeral key", "err", err)
		os.Exit(1)
	}
	return key
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// bindAddr returns the address to listen on. DOCUMCP_BIND_ADDR takes
// precedence (e.g. "0.0.0.0:8080" in containers). Otherwise the server
// binds to loopback on the configured port so a default install is not
// reachable from the network. Set DOCUMCP_BIND_ADDR to expose it.
func bindAddr(port int) string {
	if v := os.Getenv("DOCUMCP_BIND_ADDR"); v != "" {
		return v
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

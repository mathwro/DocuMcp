package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/documcp/documcp/internal/config"
)

func TestWatcher_CallsCallbackOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\nsources: []\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	called := make(chan *config.Config, 1)
	w, err := config.Watch(path, func(cfg *config.Config) {
		called <- cfg
	})
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	defer w.Stop()

	// Modify the file
	if err := os.WriteFile(path, []byte("server:\n  port: 9090\nsources: []\n"), 0644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	select {
	case cfg := <-called:
		if cfg.Server.Port != 9090 {
			t.Errorf("expected port 9090 in callback, got %d", cfg.Server.Port)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback not called within 3 seconds after file change")
	}
}

func TestWatcher_StopPreventsCallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\nsources: []\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	called := make(chan struct{}, 1)
	w, err := config.Watch(path, func(_ *config.Config) {
		called <- struct{}{}
	})
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}

	w.Stop() // Now synchronous — goroutine has fully exited before this returns

	// Write after stop — callback should NOT be called
	if err := os.WriteFile(path, []byte("server:\n  port: 1111\nsources: []\n"), 0644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	select {
	case <-called:
		t.Error("callback was called after Stop()")
	case <-time.After(500 * time.Millisecond):
		// Expected: no callback
	}
}

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mathwro/DocuMcp/internal/config"
)

func TestLoadConfig_ValidFile(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(cfg.Sources))
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := config.Load("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadConfig_SourceTypes(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	types := map[string]bool{}
	for _, s := range cfg.Sources {
		types[s.Type] = true
	}
	if !types["github_wiki"] {
		t.Error("expected github_wiki source type")
	}
	if !types["web"] {
		t.Error("expected web source type")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Write a minimal config with no server block to trigger defaults
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")
	if err := os.WriteFile(path, []byte("sources: []\n"), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.DataDir != "/app/data" {
		t.Errorf("expected default data_dir '/app/data', got %q", cfg.Server.DataDir)
	}
}

func TestLoadOrDefault_MissingFile(t *testing.T) {
	cfg, err := config.LoadOrDefault("testdata/nonexistent.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.DataDir != "/app/data" {
		t.Errorf("expected default data_dir '/app/data', got %q", cfg.Server.DataDir)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("expected zero sources, got %d", len(cfg.Sources))
	}
}

func TestLoadOrDefault_ExistingFile(t *testing.T) {
	cfg, err := config.LoadOrDefault("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(cfg.Sources))
	}
}

func TestLoadOrDefault_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	// Unbalanced bracket — yaml.Unmarshal will return an error.
	if err := os.WriteFile(path, []byte("server: {port: 8080\n"), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	_, err := config.LoadOrDefault(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

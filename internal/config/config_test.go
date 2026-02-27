package config_test

import (
	"testing"
	"github.com/documcp/documcp/internal/config"
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
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Port is set in testdata, so defaults won't trigger — just verify struct is populated
	if cfg.Server.DataDir == "" {
		t.Error("expected DataDir to be set")
	}
}

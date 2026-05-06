package crawler

import (
	"reflect"
	"testing"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

func TestSourceToConfig_github_repo_defaults_branch_to_main(t *testing.T) {
	got := sourceToConfig(db.Source{Type: "github_repo", Repo: "o/r", Branch: ""})
	if got.Branch != "main" {
		t.Errorf("Branch default: got %q, want %q", got.Branch, "main")
	}

	explicit := sourceToConfig(db.Source{Type: "github_repo", Repo: "o/r", Branch: "develop"})
	if explicit.Branch != "develop" {
		t.Errorf("Branch override: got %q, want %q", explicit.Branch, "develop")
	}
}

func TestProviderForType_github_repo(t *testing.T) {
	if providerForType("github_repo") != "github" {
		t.Errorf("expected provider 'github' for github_repo")
	}
}

func TestSourceToConfig_ForwardsIncludePaths(t *testing.T) {
	got := sourceToConfig(db.Source{
		Type:         "web",
		URL:          "https://docs.example.com",
		IncludePath:  "https://docs.example.com/legacy/",
		IncludePaths: []string{"https://docs.example.com/guides/"},
	})
	if got.IncludePath != "https://docs.example.com/legacy/" {
		t.Fatalf("IncludePath = %q", got.IncludePath)
	}
	if !reflect.DeepEqual(got.IncludePaths, []string{"https://docs.example.com/guides/"}) {
		t.Fatalf("IncludePaths = %#v", got.IncludePaths)
	}
}

func TestSyncConfigSources_InsertsConfigSourcesForUIList(t *testing.T) {
	store := testutil.OpenStore(t)

	cfg := &config.Config{Sources: []config.SourceConfig{
		{
			Name:         "Config Docs",
			Type:         "web",
			URL:          "https://docs.example.com",
			IncludePaths: []string{"guides/", "reference/"},
		},
	}}
	if err := SyncConfigSources(store, cfg); err != nil {
		t.Fatalf("SyncConfigSources: %v", err)
	}

	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(sources))
	}
	if sources[0].Name != "Config Docs" {
		t.Fatalf("source name = %q", sources[0].Name)
	}
	if !reflect.DeepEqual(sources[0].IncludePaths, []string{"guides/", "reference/"}) {
		t.Fatalf("IncludePaths = %#v", sources[0].IncludePaths)
	}
}

package crawler

import (
	"testing"

	"github.com/documcp/documcp/internal/db"
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

package githubrepo_test

import (
	"testing"

	"github.com/documcp/documcp/internal/adapter"
	_ "github.com/documcp/documcp/internal/adapter/githubrepo"
)

func TestAdapterRegistered(t *testing.T) {
	a, ok := adapter.Registry["github_repo"]
	if !ok {
		t.Fatal("github_repo adapter not registered")
	}
	if a == nil {
		t.Fatal("github_repo adapter is nil")
	}
}

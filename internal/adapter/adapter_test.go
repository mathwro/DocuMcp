package adapter

import (
	"context"
	"testing"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
)

type registryTestAdapter struct{}

func (registryTestAdapter) Crawl(context.Context, config.SourceConfig, int64) (int, <-chan db.Page, <-chan error, error) {
	pages := make(chan db.Page)
	errs := make(chan error)
	close(pages)
	close(errs)
	return 0, pages, errs, nil
}

func (registryTestAdapter) NeedsAuth(config.SourceConfig) bool { return false }

func TestRegisterAddsAdapterToRegistry(t *testing.T) {
	const sourceType = "registry_test"
	original, hadOriginal := Registry[sourceType]
	t.Cleanup(func() {
		if hadOriginal {
			Registry[sourceType] = original
		} else {
			delete(Registry, sourceType)
		}
	})

	adapter := registryTestAdapter{}
	Register(sourceType, adapter)

	got, ok := Registry[sourceType]
	if !ok {
		t.Fatal("registered adapter missing from registry")
	}
	if got == nil {
		t.Fatal("registered adapter is nil")
	}
	if got.NeedsAuth(config.SourceConfig{}) {
		t.Fatal("registered adapter returned unexpected NeedsAuth result")
	}
}

// Package testutil provides shared helpers for tests across packages.
package testutil

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/db"
)

// OpenStore opens an in-memory store for tests. If the test binary was
// built without the sqlite_fts5 tag the schema exec fails with
// "no such module: fts5"; in that case the test is skipped with a
// message pointing at the correct invocation instead of erroring out.
func OpenStore(t *testing.T) *db.Store {
	t.Helper()
	return openAt(t, ":memory:")
}

// OpenStoreFile opens a file-backed store inside t.TempDir() for tests
// that need an on-disk database. Same FTS5 skip semantics as OpenStore.
func OpenStoreFile(t *testing.T) *db.Store {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "test.db"))
}

func openAt(t *testing.T, path string) *db.Store {
	t.Helper()
	store, err := db.Open(path)
	if err != nil {
		if strings.Contains(err.Error(), "no such module: fts5") {
			t.Skip("SQLite FTS5 required; run `make test` or `go test -tags sqlite_fts5 ./...`")
		}
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

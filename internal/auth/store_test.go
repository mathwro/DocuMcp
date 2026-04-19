package auth_test

import (
	"testing"
	"time"

	"github.com/mathwro/DocuMcp/internal/auth"
	"github.com/mathwro/DocuMcp/internal/db"
)

func TestTokenStore_SaveAndLoad(t *testing.T) {
	dbStore, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbStore.Close()

	// Insert a source row so the foreign key constraint on tokens is satisfied.
	srcID, err := dbStore.InsertSource(db.Source{
		Name: "test-source",
		Type: "web",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	key := []byte("test-encryption-key-32-bytes!!!!")
	ts := auth.NewTokenStore(dbStore, key)
	token := &auth.Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}

	if err := ts.Save(srcID, "microsoft", token); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := ts.Load(srcID, "microsoft")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("AccessToken: expected %q, got %q", token.AccessToken, loaded.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken: expected %q, got %q", token.RefreshToken, loaded.RefreshToken)
	}
}

func TestTokenStore_LoadNotFound(t *testing.T) {
	dbStore, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dbStore.Close()

	key := []byte("test-encryption-key-32-bytes!!!!")
	ts := auth.NewTokenStore(dbStore, key)

	_, err = ts.Load(99, "microsoft")
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
}

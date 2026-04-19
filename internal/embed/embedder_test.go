package embed_test

import (
	"os"
	"testing"

	"github.com/mathwro/DocuMcp/internal/embed"
)

func TestEmbedder_ProducesVectors(t *testing.T) {
	modelPath := os.Getenv("DOCUMCP_MODEL_PATH")
	if modelPath == "" {
		t.Skip("DOCUMCP_MODEL_PATH not set — skipping embedding test")
	}
	e, err := embed.New(modelPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	vecs, err := e.Embed([]string{"hello world", "test sentence"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) == 0 {
		t.Error("expected non-empty vector")
	}
}

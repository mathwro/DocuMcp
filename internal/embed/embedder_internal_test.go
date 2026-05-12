package embed

import (
	"errors"
	"testing"

	"github.com/knights-analytics/hugot/pipelines"
)

type fakePipeline struct {
	output *pipelines.FeatureExtractionOutput
	err    error
}

func (f *fakePipeline) RunPipeline(_ []string) (*pipelines.FeatureExtractionOutput, error) {
	return f.output, f.err
}

type fakeSession struct {
	err       error
	destroyed bool
}

func (f *fakeSession) Destroy() error {
	f.destroyed = true
	return f.err
}

func TestEmbedCopiesPipelineEmbeddings(t *testing.T) {
	embedding := []float32{1, 2, 3}
	e := &Embedder{
		pipeline: &fakePipeline{output: &pipelines.FeatureExtractionOutput{
			Embeddings: [][]float32{embedding},
		}},
	}

	got, err := e.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d vectors, want 1", len(got))
	}
	if got[0][0] != 1 || got[0][1] != 2 || got[0][2] != 3 {
		t.Fatalf("Embed() = %#v", got)
	}

	embedding[0] = 99
	if got[0][0] != 1 {
		t.Fatalf("Embed returned aliased vector; got[0][0] = %v, want 1", got[0][0])
	}
}

func TestEmbedWrapsPipelineError(t *testing.T) {
	e := &Embedder{pipeline: &fakePipeline{err: errors.New("pipeline failed")}}

	_, err := e.Embed([]string{"hello"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, e.pipeline.(*fakePipeline).err) {
		t.Fatalf("Embed error = %v, want wrapped pipeline error", err)
	}
}

func TestCloseDestroysSession(t *testing.T) {
	session := &fakeSession{}
	e := &Embedder{session: session}

	e.Close()

	if !session.destroyed {
		t.Fatal("Close did not destroy session")
	}
}

// Package embed provides an ONNX embedding wrapper using hugot.
// It is used to generate vector embeddings from text for semantic search.
package embed

import (
	"fmt"
	"log/slog"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// Embedder wraps a hugot feature extraction pipeline.
type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
}

// New creates an Embedder backed by the ONNX model at modelPath.
// modelPath must be a directory containing model.onnx and tokenizer files.
func New(modelPath string) (*Embedder, error) {
	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, fmt.Errorf("hugot session: %w", err)
	}
	pipeline, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         "embedder",
		OnnxFilename: "model.onnx",
	})
	if err != nil {
		_ = session.Destroy()
		return nil, fmt.Errorf("embedding pipeline: %w", err)
	}
	return &Embedder{session: session, pipeline: pipeline}, nil
}

// Embed returns one embedding vector per input text.
// Vectors are the raw mean-pooled output of the model; normalization is NOT
// applied. For cosine similarity (as used by sqlite-vec), either normalize
// here with hugot.WithNormalization() or use sqlite-vec's vec_distance_cosine
// function rather than dot product.
func (e *Embedder) Embed(texts []string) ([][]float32, error) {
	output, err := e.pipeline.RunPipeline(texts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	vecs := make([][]float32, len(output.Embeddings))
	for i, emb := range output.Embeddings {
		dst := make([]float32, len(emb))
		copy(dst, emb)
		vecs[i] = dst
	}
	return vecs, nil
}

// Close releases all resources associated with the embedder.
func (e *Embedder) Close() {
	if err := e.session.Destroy(); err != nil {
		slog.Warn("embed: session destroy", "err", err)
	}
}

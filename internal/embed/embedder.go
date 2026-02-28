// Package embed provides an ONNX embedding wrapper using hugot.
// It is used to generate vector embeddings from text for semantic search.
package embed

import (
	"fmt"

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
// Vectors are L2-normalised by the pipeline (WithNormalization option is not
// set here; normalization can be added via pipeline options if desired).
func (e *Embedder) Embed(texts []string) ([][]float32, error) {
	output, err := e.pipeline.RunPipeline(texts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	vecs := make([][]float32, len(output.Embeddings))
	copy(vecs, output.Embeddings)
	return vecs, nil
}

// Close releases all resources associated with the embedder.
func (e *Embedder) Close() {
	_ = e.session.Destroy()
}

// internal/bench/report/json.go
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

type Metadata struct {
	Model          string    `json:"model"`
	DocuMcpVersion string    `json:"documcp_version"`
	GitSHA         string    `json:"git_sha"`
	CorpusHash     string    `json:"corpus_hash"`
	Timestamp      time.Time `json:"timestamp"`
}

type Report struct {
	Metadata Metadata              `json:"metadata"`
	PageDiff *pagediff.Result      `json:"page_diff,omitempty"`
	Trials   []tasks.TrialResult   `json:"trials,omitempty"`
	Tiers    map[string]int        `json:"tiers,omitempty"` // questionID → tier
	Judge    tasks.JudgeAccounting `json:"judge"`
}

func WriteJSON(path string, rep Report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// internal/bench/report/report_test.go
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func TestWriteJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rep := Report{
		Metadata: Metadata{Model: "claude-sonnet-4-6", DocuMcpVersion: "test"},
		PageDiff: &pagediff.Result{Rows: []pagediff.Row{{URL: "u", TokensRaw: 100, TokensStripped: 50, TokensDocuMcp: 10, RatioStrippedOverDoc: 5, RatioRawOverDoc: 10}}},
		Trials: []tasks.TrialResult{
			{QuestionID: "q1", Config: "A", Trial: 1, InputTokens: 100, OutputTokens: 50, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 1, InputTokens: 30, OutputTokens: 10, Correct: true},
		},
	}
	if err := WriteJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "results.json"))
	var got Report
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Metadata.Model != "claude-sonnet-4-6" {
		t.Errorf("metadata round-trip failed: %+v", got.Metadata)
	}
	if len(got.Trials) != 2 {
		t.Errorf("trials round-trip failed: got %d", len(got.Trials))
	}
}

func TestWriteMarkdown_IncludesHeadlineAndPerTier(t *testing.T) {
	dir := t.TempDir()
	rep := Report{
		Metadata: Metadata{Model: "claude-sonnet-4-6"},
		Trials: []tasks.TrialResult{
			{QuestionID: "q1", Config: "A", Trial: 1, InputTokens: 200, OutputTokens: 50, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 1, InputTokens: 30, OutputTokens: 10, Correct: true},
			{QuestionID: "q1", Config: "A", Trial: 2, InputTokens: 220, OutputTokens: 60, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 2, InputTokens: 35, OutputTokens: 12, Correct: true},
		},
		Tiers: map[string]int{"q1": 1},
	}
	path := filepath.Join(dir, "summary.md")
	if err := WriteMarkdown(path, rep); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	for _, want := range []string{"Headline", "Config A", "Config B", "Tier 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, s)
		}
	}
}

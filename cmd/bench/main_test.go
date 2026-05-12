package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTasksJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	data := []byte(`{"id":"k8s-readiness","prompt":"How do I configure a readiness probe?","expected_contains":["readinessProbe"],"expected_url_contains":["/readiness"]}
{"id":"go-context","prompt":"How do I cancel a context?","source":"Go"}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write tasks: %v", err)
	}

	tasks, err := loadTasks(path)
	if err != nil {
		t.Fatalf("load tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "k8s-readiness" || tasks[0].ExpectedContains[0] != "readinessProbe" {
		t.Fatalf("unexpected first task: %+v", tasks[0])
	}
	if tasks[1].Source != "Go" {
		t.Fatalf("expected source Go, got %q", tasks[1].Source)
	}
}

func TestParseFlagsValidation(t *testing.T) {
	if _, err := parseFlags(nil); err == nil || !strings.Contains(err.Error(), "-tasks is required") {
		t.Fatalf("parseFlags without tasks error = %v, want -tasks required", err)
	}
	if _, err := parseFlags([]string{"-tasks", "tasks.jsonl", "-runs", "0"}); err == nil || !strings.Contains(err.Error(), "-runs") {
		t.Fatalf("parseFlags invalid runs error = %v, want runs validation", err)
	}
	if _, err := parseFlags([]string{"-tasks", "tasks.jsonl", "-mode", "unknown"}); err == nil || !strings.Contains(err.Error(), "unsupported -mode") {
		t.Fatalf("parseFlags invalid mode error = %v, want unsupported mode", err)
	}

	cfg, err := parseFlags([]string{"-tasks", "tasks.jsonl", "-mode", "native", "-runs", "2", "-timeout", "5s", "-dry-run"})
	if err != nil {
		t.Fatalf("parseFlags valid args: %v", err)
	}
	if cfg.TasksPath != "tasks.jsonl" || cfg.Mode != "native" || cfg.Runs != 2 || cfg.Timeout != 5*time.Second || !cfg.DryRun {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestRunDryRunWritesReport(t *testing.T) {
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.jsonl")
	outputPath := filepath.Join(dir, "report.json")
	if err := os.WriteFile(tasksPath, []byte(`{"id":"dry","prompt":"Say hello","expected_contains":["codex"]}
`), 0o600); err != nil {
		t.Fatalf("write tasks: %v", err)
	}

	var out strings.Builder
	err := run(context.Background(), runConfig{
		CodexCommand: "codex",
		TasksPath:    tasksPath,
		Mode:         "native",
		Workdir:      dir,
		OutputPath:   outputPath,
		RawDir:       "",
		Runs:         1,
		Timeout:      time.Second,
		DryRun:       true,
	}, &out)
	if err != nil {
		t.Fatalf("run dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry native run 1") || !strings.Contains(out.String(), "wrote "+outputPath) {
		t.Fatalf("unexpected output:\n%s", out.String())
	}

	var rep report
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("report results = %d, want 1", len(rep.Results))
	}
	if !strings.Contains(rep.Results[0].FinalAnswer, "codex --search exec") {
		t.Fatalf("dry-run final answer = %q, want command line", rep.Results[0].FinalAnswer)
	}
}

func TestAnalyzeCodexEventsExtractsUsageAndToolCalls(t *testing.T) {
	jsonl := []byte(`{"type":"response.started"}
{"type":"web_search_call","action":{"type":"search","query":"readiness probe kubernetes"}}
{"type":"mcp_tool_call","tool_name":"search_docs","arguments":{"query":"readiness probe"}}
{"type":"response.completed","response":{"usage":{"input_tokens":1200,"output_tokens":180,"total_tokens":1380},"cost_usd":0.0042}}
`)

	metrics := analyzeCodexEvents(jsonl)
	if metrics.EventCount != 4 {
		t.Fatalf("expected 4 events, got %d", metrics.EventCount)
	}
	if metrics.WebSearchCalls != 1 {
		t.Fatalf("expected 1 web search call, got %d", metrics.WebSearchCalls)
	}
	if metrics.MCPToolCalls != 1 {
		t.Fatalf("expected 1 MCP tool call, got %d", metrics.MCPToolCalls)
	}
	if metrics.InputTokens != 1200 || metrics.OutputTokens != 180 || metrics.TotalTokens != 1380 {
		t.Fatalf("unexpected token metrics: %+v", metrics)
	}
	if metrics.CostUSD != 0.0042 {
		t.Fatalf("expected cost 0.0042, got %f", metrics.CostUSD)
	}
}

func TestAnalyzeCodexEventsDerivesTotalTokens(t *testing.T) {
	jsonl := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":1200,"cached_input_tokens":400,"output_tokens":180}}}
`)

	metrics := analyzeCodexEvents(jsonl)
	if metrics.TotalTokens != 1380 {
		t.Fatalf("expected total tokens to be derived as 1380, got %d", metrics.TotalTokens)
	}
	if metrics.CachedInputTokens != 400 {
		t.Fatalf("expected cached input tokens 400, got %d", metrics.CachedInputTokens)
	}
}

func TestBuildCodexArgs(t *testing.T) {
	cfg := runConfig{
		CodexCommand: "codex",
		Model:        "gpt-5.3-codex",
		Workdir:      "/tmp/work",
		DocuMCPURL:   "http://localhost:8080/mcp/http",
	}

	args := buildCodexArgs(cfg, "native", "/tmp/final.txt", "Answer this")
	want := []string{"--search", "exec", "--ignore-user-config", "--json", "-o", "/tmp/final.txt", "-C", "/tmp/work", "-m", "gpt-5.3-codex", "Answer this"}
	if len(args) != len(want) {
		t.Fatalf("arg length: expected %d, got %d: %#v", len(want), len(args), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg %d: expected %q, got %q; args=%#v", i, want[i], args[i], args)
		}
	}

	docArgs := buildCodexArgs(cfg, "documcp", "/tmp/final.txt", "Answer this")
	foundIgnoreUserConfig := false
	foundConfig := false
	for _, arg := range docArgs {
		if arg == "--search" {
			t.Fatalf("documcp args should not enable native search: %#v", docArgs)
		}
		if arg == "--ignore-user-config" {
			foundIgnoreUserConfig = true
		}
		if arg == `mcp_servers.documcp.url=""` {
			foundConfig = true
		}
	}
	if !foundIgnoreUserConfig {
		t.Fatalf("documcp args should ignore user config to isolate the benchmark: %#v", docArgs)
	}
	if foundConfig {
		t.Fatalf("documcp args should include the configured URL, got empty URL: %#v", docArgs)
	}
	foundConfig = false
	for _, arg := range docArgs {
		if arg == `mcp_servers.documcp.url="http://localhost:8080/mcp/http"` {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("documcp args should include MCP config override: %#v", docArgs)
	}

	noopArgs := buildCodexArgs(cfg, "mcp_noop", "/tmp/final.txt", "Say OK")
	for _, arg := range noopArgs {
		if arg == "--search" {
			t.Fatalf("mcp_noop args should not enable native search: %#v", noopArgs)
		}
	}
	foundConfig = false
	for _, arg := range noopArgs {
		if arg == `mcp_servers.documcp.url="http://localhost:8080/mcp/http"` {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("mcp_noop args should attach DocuMcp: %#v", noopArgs)
	}
}

func TestPromptForDocuMCPConstrainsToolUse(t *testing.T) {
	prompt := promptFor(task{
		Prompt: "How do I configure a readiness probe?",
		Source: "Kubernetes",
	}, "documcp")

	for _, want := range []string{
		"Do not call list_sources",
		"Call search_docs exactly once",
		`source="Kubernetes"`,
		"At most one get_page_excerpt call",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPromptForMCPNoopForbidsTools(t *testing.T) {
	prompt := promptFor(task{Prompt: "ignored"}, "mcp_noop")
	for _, want := range []string{"Reply exactly: OK", "Do not call any tools"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestModes(t *testing.T) {
	cases := []struct {
		mode string
		want []string
	}{
		{mode: "native", want: []string{"native"}},
		{mode: "documcp", want: []string{"documcp"}},
		{mode: "mcp_noop", want: []string{"mcp_noop"}},
		{mode: "both", want: []string{"native", "documcp"}},
		{mode: "all", want: []string{"native", "documcp", "mcp_noop"}},
	}
	for _, tc := range cases {
		got := modesFor(tc.mode)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("modesFor(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func TestRawEventsPath(t *testing.T) {
	got := rawEventsPath("/tmp/raw", result{TaskID: "go/http db", Mode: "mcp_noop", Run: 2})
	want := filepath.Join("/tmp/raw", "go-http-db__mcp_noop__run-2.jsonl")
	if got != want {
		t.Fatalf("rawEventsPath = %q, want %q", got, want)
	}
}

func TestWriteRawEventsCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw", "events.jsonl")

	if err := writeRawEvents(path, []byte("event\n")); err != nil {
		t.Fatalf("writeRawEvents: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw events: %v", err)
	}
	if string(got) != "event\n" {
		t.Fatalf("raw events = %q, want event newline", got)
	}
	if err := writeRawEvents("", []byte("ignored")); err != nil {
		t.Fatalf("writeRawEvents empty path: %v", err)
	}
}

func TestSanitizeFilenameFallbackAndCompaction(t *testing.T) {
	if got := sanitizeFilename("  Hello, World!  "); got != "hello-world" {
		t.Fatalf("sanitizeFilename = %q, want hello-world", got)
	}
	if got := sanitizeFilename("!!!"); got != "run" {
		t.Fatalf("sanitizeFilename punctuation = %q, want run", got)
	}
}

func TestComparePairsReportsRatios(t *testing.T) {
	pairs := comparePairs([]result{
		{TaskID: "one", Mode: "native", DurationMS: 100, Metrics: eventMetrics{TotalTokens: 200}},
		{TaskID: "one", Mode: "documcp", DurationMS: 150, Metrics: eventMetrics{TotalTokens: 300}},
	})
	if len(pairs) != 1 {
		t.Fatalf("expected one comparison, got %d", len(pairs))
	}
	if pairs[0].TokenRatio != 1.5 {
		t.Fatalf("expected token ratio 1.5, got %f", pairs[0].TokenRatio)
	}
	if pairs[0].DurationRatio != 1.5 {
		t.Fatalf("expected duration ratio 1.5, got %f", pairs[0].DurationRatio)
	}
}

func TestMCPNoopSuccess(t *testing.T) {
	r := result{
		Mode:        "mcp_noop",
		ExitCode:    0,
		FinalAnswer: "OK",
		Metrics:     eventMetrics{ToolCalls: 0},
	}
	if !isSuccessful(r) {
		t.Fatal("expected mcp_noop OK with no tools to count as successful")
	}
	r.Metrics.ToolCalls = 1
	if isSuccessful(r) {
		t.Fatal("expected mcp_noop with tool calls to fail")
	}
}

func TestEvaluateHintsReportsMissingExpectedContent(t *testing.T) {
	hints := evaluateHints(task{
		ExpectedContains:    []string{"readinessProbe", "livenessProbe"},
		ExpectedURLContains: []string{"/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/"},
	}, "Use readinessProbe for readiness checks.", "https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/")

	if hints.AnswerContainsMatched {
		t.Fatal("expected answer hints not to fully match")
	}
	if len(hints.MissingAnswerContains) != 1 || hints.MissingAnswerContains[0] != "livenessProbe" {
		t.Fatalf("missing answer hints = %#v", hints.MissingAnswerContains)
	}
	if !hints.URLContainsMatched {
		t.Fatalf("expected URL hint to match, missing %#v", hints.MissingURLContains)
	}
}

func TestSummarizeComputesMediansAndSuccessRate(t *testing.T) {
	summaries := summarize([]result{
		{
			Mode:       "native",
			DurationMS: 300,
			ExitCode:   0,
			Metrics:    eventMetrics{InputTokens: 30, TotalTokens: 300, ToolCalls: 2},
			CorrectHints: hintResult{
				AnswerContainsMatched: true,
				URLContainsMatched:    true,
			},
		},
		{
			Mode:       "native",
			DurationMS: 100,
			ExitCode:   1,
			Metrics:    eventMetrics{InputTokens: 10, TotalTokens: 100, ToolCalls: 1},
			CorrectHints: hintResult{
				AnswerContainsMatched: true,
				URLContainsMatched:    true,
			},
		},
		{
			Mode:       "native",
			DurationMS: 200,
			ExitCode:   0,
			Metrics:    eventMetrics{InputTokens: 20, TotalTokens: 200, ToolCalls: 3},
			CorrectHints: hintResult{
				AnswerContainsMatched: true,
				URLContainsMatched:    true,
			},
		},
	})

	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
	got := summaries[0]
	if got.Runs != 3 || got.MedianDurationMS != 200 || got.MedianInputTokens != 20 || got.MedianTotalTokens != 200 || got.MedianToolCalls != 2 {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if got.Successes != 2 || got.SuccessRate != 2.0/3.0 {
		t.Fatalf("success summary = %d/%f, want 2/%f", got.Successes, got.SuccessRate, 2.0/3.0)
	}
}

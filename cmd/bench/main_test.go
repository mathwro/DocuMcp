package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// internal/bench/tasks/corpus_test.go
package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadCorpus_ValidatesFields(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "good.json", `[
		{"id":"q1","tier":1,"question":"q?","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	qs, err := LoadCorpus(good, map[string]bool{"src": true})
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(qs) != 1 || qs[0].ID != "q1" {
		t.Fatalf("unexpected qs: %+v", qs)
	}
}

func TestLoadCorpus_RejectsUnknownSource(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q1","tier":1,"question":"q?","expected_source":"missing","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLoadCorpus_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":1,"question":"a","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"},
		{"id":"q","tier":2,"question":"b","expected_source":"src","expected_url_pattern":"y","reference_excerpt":"f"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoadCorpus_RejectsBadTier(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":4,"question":"a","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for tier 4")
	}
}

func TestLoadCorpus_RejectsBadRegex(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":1,"question":"a","expected_source":"src","expected_url_pattern":"[unbalanced","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for bad regex")
	}
}

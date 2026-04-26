// internal/bench/tokens/count_test.go
package tokens

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCount_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("missing or wrong api key header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": 42})
	}))
	defer srv.Close()

	c := New("test-key", "claude-sonnet-4-6", WithBaseURL(srv.URL))
	got, err := c.Count(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
}

func TestCount_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := New("test-key", "claude-sonnet-4-6", WithBaseURL(srv.URL))
	if _, err := c.Count(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

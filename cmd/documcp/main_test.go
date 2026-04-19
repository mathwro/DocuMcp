package main

import (
	"testing"
)

func TestBindAddr_DefaultsToLoopback(t *testing.T) {
	t.Setenv("DOCUMCP_BIND_ADDR", "")
	got := bindAddr(8080)
	if got != "127.0.0.1:8080" {
		t.Fatalf("bindAddr(8080) = %q, want 127.0.0.1:8080", got)
	}
}

func TestBindAddr_EnvOverride(t *testing.T) {
	t.Setenv("DOCUMCP_BIND_ADDR", "0.0.0.0:9090")
	got := bindAddr(8080)
	if got != "0.0.0.0:9090" {
		t.Fatalf("bindAddr with override = %q, want 0.0.0.0:9090", got)
	}
}

package main

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
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

func TestDeriveKey_HexSecret(t *testing.T) {
	want := []byte("12345678901234567890123456789012")
	t.Setenv("DOCUMCP_SECRET_KEY", hex.EncodeToString(want))

	got := deriveKey()
	if string(got) != string(want) {
		t.Fatalf("deriveKey() = %x, want %x", got, want)
	}
}

func TestDeriveKey_Base64Secret(t *testing.T) {
	want := []byte("abcdefghijklmnopqrstuvwxyz123456")
	t.Setenv("DOCUMCP_SECRET_KEY", base64.StdEncoding.EncodeToString(want))

	got := deriveKey()
	if string(got) != string(want) {
		t.Fatalf("deriveKey() = %q, want %q", got, want)
	}
}

func TestDeriveKey_URLBase64Secret(t *testing.T) {
	want := []byte(strings.Repeat("\xff", 32))
	t.Setenv("DOCUMCP_SECRET_KEY", base64.URLEncoding.EncodeToString(want))

	got := deriveKey()
	if string(got) != string(want) {
		t.Fatalf("deriveKey() = %x, want %x", got, want)
	}
}

func TestDeriveKey_InvalidSecretFallsBackToEphemeralKey(t *testing.T) {
	t.Setenv("DOCUMCP_SECRET_KEY", "not-a-valid-32-byte-key")

	got := deriveKey()
	if len(got) != 32 {
		t.Fatalf("deriveKey() length = %d, want 32", len(got))
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("DOCUMCP_TEST_VALUE", "configured")
	if got := getenv("DOCUMCP_TEST_VALUE", "default"); got != "configured" {
		t.Fatalf("getenv existing = %q, want configured", got)
	}
	if got := getenv("DOCUMCP_TEST_MISSING", "default"); got != "default" {
		t.Fatalf("getenv missing = %q, want default", got)
	}
}

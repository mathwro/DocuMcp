package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/documcp/documcp/internal/auth"
)

func TestGitHubDeviceFlow_InitiatesSuccessfully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "gh-test-device-code",
			"user_code":        "WXYZ-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow, err := auth.NewGitHubDeviceFlow(srv.URL, "test-client-id")
	if err != nil {
		t.Fatalf("NewGitHubDeviceFlow: %v", err)
	}
	if flow.DeviceCode == "" {
		t.Error("expected DeviceCode to be populated")
	}
	if flow.UserCode == "" {
		t.Error("expected UserCode to be populated")
	}
	if flow.VerificationURI == "" {
		t.Error("expected VerificationURI to be populated")
	}
}

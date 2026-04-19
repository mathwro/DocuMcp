package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathwro/DocuMcp/internal/auth"
)

func TestMicrosoftDeviceFlow_InitiatesSuccessfully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://microsoft.com/devicelogin",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow, err := auth.NewMicrosoftDeviceFlow(srv.URL, "consumers")
	if err != nil {
		t.Fatalf("NewMicrosoftDeviceFlow: %v", err)
	}
	if flow.UserCode == "" {
		t.Error("expected UserCode to be populated")
	}
	if flow.VerificationURI == "" {
		t.Error("expected VerificationURI to be populated")
	}
	if flow.DeviceCode == "" {
		t.Error("expected DeviceCode to be populated")
	}
}

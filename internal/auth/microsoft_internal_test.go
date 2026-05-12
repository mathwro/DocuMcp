package auth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMicrosoftDeviceFlowPollReturnsTokenAfterPending(t *testing.T) {
	restore := stubPollDependencies(t, []pollResponse{
		{body: `{"error":"authorization_pending"}`},
		{body: `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`},
	})
	defer restore()

	flow := &MicrosoftDeviceFlow{
		DeviceCode:    "device-code",
		Interval:      1,
		tokenEndpoint: "https://login.example/token",
	}
	token, err := flow.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("token = %#v", token)
	}
	if time.Until(token.ExpiresAt) <= 0 {
		t.Fatalf("ExpiresAt = %v, want future time", token.ExpiresAt)
	}
}

func TestMicrosoftDeviceFlowPollReturnsAuthError(t *testing.T) {
	restore := stubPollDependencies(t, []pollResponse{
		{body: `{"error":"expired_token"}`},
	})
	defer restore()

	flow := &MicrosoftDeviceFlow{DeviceCode: "device-code", tokenEndpoint: "https://login.example/token"}
	_, err := flow.Poll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expired_token") {
		t.Fatalf("Poll error = %v, want expired_token auth error", err)
	}
}

func TestMicrosoftDeviceFlowPollReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flow := &MicrosoftDeviceFlow{DeviceCode: "device-code", tokenEndpoint: "https://login.example/token"}
	_, err := flow.Poll(ctx)
	if err != context.Canceled {
		t.Fatalf("Poll error = %v, want context.Canceled", err)
	}
}

type pollResponse struct {
	body string
}

func stubPollDependencies(t *testing.T, responses []pollResponse) func() {
	t.Helper()
	originalAfter := pollAfter
	originalPostForm := postForm
	pollAfter = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	call := 0
	postForm = func(endpoint string, values url.Values) (*http.Response, error) {
		if endpoint != "https://login.example/token" {
			t.Fatalf("endpoint = %q, want token endpoint", endpoint)
		}
		if values.Get("client_id") != AzureCLIClientID {
			t.Fatalf("client_id = %q, want Azure CLI client ID", values.Get("client_id"))
		}
		if values.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Fatalf("grant_type = %q", values.Get("grant_type"))
		}
		if values.Get("device_code") != "device-code" {
			t.Fatalf("device_code = %q", values.Get("device_code"))
		}
		if call >= len(responses) {
			t.Fatalf("unexpected poll call %d", call+1)
		}
		resp := responses[call]
		call++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(resp.body)),
			Header:     make(http.Header),
		}, nil
	}
	return func() {
		pollAfter = originalAfter
		postForm = originalPostForm
	}
}

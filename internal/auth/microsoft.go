package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// AzureCLIClientID is the well-known public client ID used by Azure CLI.
// No app registration required — works across all tenants without admin consent.
const AzureCLIClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"

// MicrosoftDeviceFlow holds state for an in-progress device code flow.
type MicrosoftDeviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        int
	tokenEndpoint   string
}

// NewMicrosoftDeviceFlow initiates a device code flow against the given base URL
// and tenant. For production use: baseURL="https://login.microsoftonline.com", tenant="consumers".
func NewMicrosoftDeviceFlow(baseURL, tenant string) (*MicrosoftDeviceFlow, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", baseURL, tenant)
	resp, err := http.PostForm(endpoint, url.Values{
		"client_id": {AzureCLIClientID},
		"scope":     {"https://management.azure.com/user_impersonation offline_access"},
	})
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	tokenEndpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", baseURL, tenant)
	return &MicrosoftDeviceFlow{
		DeviceCode:      result.DeviceCode,
		UserCode:        result.UserCode,
		VerificationURI: result.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		Interval:        result.Interval,
		tokenEndpoint:   tokenEndpoint,
	}, nil
}

// Poll waits for the user to complete the device code flow, polling the token
// endpoint at the specified interval. Blocks until a token is received, the
// flow expires, or ctx is cancelled.
func (f *MicrosoftDeviceFlow) Poll(ctx context.Context) (*Token, error) {
	interval := f.Interval
	if interval <= 0 {
		interval = 5
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		resp, err := http.PostForm(f.tokenEndpoint, url.Values{
			"client_id":   {AzureCLIClientID},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {f.DeviceCode},
		})
		if err != nil {
			continue // transient error, keep polling
		}

		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		switch result.Error {
		case "authorization_pending", "slow_down":
			continue
		case "":
			if result.AccessToken != "" {
				return &Token{
					AccessToken:  result.AccessToken,
					RefreshToken: result.RefreshToken,
					ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
				}, nil
			}
		default:
			return nil, fmt.Errorf("auth error: %s", result.Error)
		}
	}
}

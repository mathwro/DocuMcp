package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitHubDeviceFlow holds state for an in-progress GitHub device code flow.
type GitHubDeviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        int
	tokenEndpoint   string
}

// NewGitHubDeviceFlow initiates a GitHub device code flow.
// baseURL is injectable for tests; use "https://github.com" for production.
func NewGitHubDeviceFlow(baseURL, clientID string) (*GitHubDeviceFlow, error) {
	endpoint := baseURL + "/login/device/code"
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build device code request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	form := url.Values{
		"client_id": {clientID},
		"scope":     {"repo"},
	}
	req.URL.RawQuery = form.Encode()

	resp, err := http.DefaultClient.Do(req)
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
	return &GitHubDeviceFlow{
		DeviceCode:      result.DeviceCode,
		UserCode:        result.UserCode,
		VerificationURI: result.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		Interval:        result.Interval,
		tokenEndpoint:   baseURL + "/login/oauth/access_token",
	}, nil
}

// Poll polls the GitHub token endpoint until the user completes the flow or ctx is cancelled.
func (f *GitHubDeviceFlow) Poll(ctx context.Context, clientID string) (*Token, error) {
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

		req, err := http.NewRequest("POST", f.tokenEndpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		form := url.Values{
			"client_id":   {clientID},
			"device_code": {f.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		req.URL.RawQuery = form.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		var result struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		switch result.Error {
		case "authorization_pending", "slow_down":
			continue
		case "":
			if result.AccessToken != "" {
				return &Token{
					AccessToken: result.AccessToken,
					ExpiresAt:   time.Now().Add(8 * time.Hour), // GitHub tokens don't expire by default
				}, nil
			}
		default:
			return nil, fmt.Errorf("auth error: %s", result.Error)
		}
	}
}

// LoadGitHubTokenFromCLI reads the GitHub token from the gh CLI config file (~/.config/gh/hosts.yml).
// Returns empty string (no error) if the file doesn't exist.
func LoadGitHubTokenFromCLI() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	hostsPath := filepath.Join(home, ".config", "gh", "hosts.yml")
	data, err := os.ReadFile(hostsPath)
	if os.IsNotExist(err) {
		return "", nil // not configured, that's fine
	}
	if err != nil {
		return "", fmt.Errorf("read gh hosts: %w", err)
	}
	const marker = "  oauth_token: "
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) > len(marker) && line[:len(marker)] == marker {
			return line[len(marker):], nil
		}
	}
	return "", nil
}

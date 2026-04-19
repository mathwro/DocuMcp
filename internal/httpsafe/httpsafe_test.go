package httpsafe_test

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/mathwro/DocuMcp/internal/httpsafe"
)

func withLookup(t *testing.T, ips map[string][]net.IP) {
	t.Helper()
	original := httpsafe.LookupHostIPs
	httpsafe.LookupHostIPs = func(_ context.Context, host string) ([]net.IP, error) {
		if v, ok := ips[host]; ok {
			return v, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { httpsafe.LookupHostIPs = original })
}

func TestAllowedHost_IPLiterals(t *testing.T) {
	cases := []struct {
		host  string
		allow bool
	}{
		{"1.1.1.1", true},
		{"127.0.0.1", false},
		{"10.0.0.5", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"::1", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"fd00:ec2::254", false},
		{"2001:4860:4860::8888", true},
	}
	for _, tc := range cases {
		var raw string
		if ip := net.ParseIP(tc.host); ip != nil && ip.To4() == nil {
			raw = "http://[" + tc.host + "]"
		} else {
			raw = "http://" + tc.host
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := httpsafe.AllowedHost(context.Background(), u); got != tc.allow {
			t.Errorf("AllowedHost(%q) = %v, want %v", tc.host, got, tc.allow)
		}
	}
}

func TestAllowedHost_HostnameResolvingToPrivate(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"evil.example.com": {net.ParseIP("127.0.0.1")},
	})
	u, _ := url.Parse("http://evil.example.com/")
	if httpsafe.AllowedHost(context.Background(), u) {
		t.Errorf("hostname resolving to 127.0.0.1 must be blocked")
	}
}

func TestAllowedHost_HostnameResolvingToMetadata(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"metadata.example.com": {net.ParseIP("169.254.169.254")},
	})
	u, _ := url.Parse("http://metadata.example.com/")
	if httpsafe.AllowedHost(context.Background(), u) {
		t.Errorf("hostname resolving to 169.254.169.254 must be blocked")
	}
}

func TestAllowedHost_HostnameResolvingToPublic(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"ok.example.com": {net.ParseIP("1.2.3.4")},
	})
	u, _ := url.Parse("http://ok.example.com/")
	if !httpsafe.AllowedHost(context.Background(), u) {
		t.Errorf("hostname resolving to public IP must be allowed")
	}
}

func TestAllowedHost_AnyResolvedIPBlockedRejectsHost(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"mixed.example.com": {
			net.ParseIP("1.2.3.4"),
			net.ParseIP("127.0.0.1"),
		},
	})
	u, _ := url.Parse("http://mixed.example.com/")
	if httpsafe.AllowedHost(context.Background(), u) {
		t.Errorf("hostname with any private resolved IP must be blocked")
	}
}

func TestAllowedHost_UnresolvedHostnameBlocked(t *testing.T) {
	withLookup(t, map[string][]net.IP{})
	u, _ := url.Parse("http://nowhere.example.com/")
	if httpsafe.AllowedHost(context.Background(), u) {
		t.Errorf("hostname that fails to resolve must be blocked (fail closed)")
	}
}

func TestCheckRedirect_AllowsPublicTarget(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"public.example.com": {net.ParseIP("1.2.3.4")},
	})
	target, _ := http.NewRequest(http.MethodGet, "https://public.example.com/page", nil)
	if err := httpsafe.CheckRedirect(target, nil); err != nil {
		t.Errorf("unexpected error for public redirect: %v", err)
	}
}

func TestCheckRedirect_BlocksLoopbackTarget(t *testing.T) {
	target, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	if err := httpsafe.CheckRedirect(target, nil); err == nil {
		t.Errorf("expected error when redirecting to 127.0.0.1")
	}
}

func TestCheckRedirect_BlocksResolvedPrivateTarget(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"evil.example.com": {net.ParseIP("10.0.0.1")},
	})
	target, _ := http.NewRequest(http.MethodGet, "http://evil.example.com/", nil)
	if err := httpsafe.CheckRedirect(target, nil); err == nil {
		t.Errorf("expected error when redirecting to RFC1918")
	}
}

func TestCheckRedirect_BlocksMetadataTarget(t *testing.T) {
	target, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err := httpsafe.CheckRedirect(target, nil); err == nil {
		t.Errorf("expected error when redirecting to cloud-metadata IP")
	}
}

func TestCheckRedirect_CapsRedirectCount(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"ok.example.com": {net.ParseIP("1.2.3.4")},
	})
	target, _ := http.NewRequest(http.MethodGet, "http://ok.example.com/", nil)
	via := make([]*http.Request, 11)
	for i := range via {
		via[i] = target
	}
	if err := httpsafe.CheckRedirect(target, via); err == nil {
		t.Errorf("expected error when redirect count exceeds limit")
	}
}

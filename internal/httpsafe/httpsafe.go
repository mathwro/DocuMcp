// Package httpsafe provides SSRF-safe HTTP helpers for the adapters.
// It centralises the host-allowlist check and a CheckRedirect hook so
// every crawl client applies the same policy.
package httpsafe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// LookupHostIPs is overridable for tests. Production calls resolve via
// net.DefaultResolver.
var LookupHostIPs = func(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, len(addrs))
	for i, a := range addrs {
		out[i] = a.IP
	}
	return out, nil
}

// AllowedHost reports whether u's host is safe to connect to. Hostnames
// are resolved and every returned IP is checked against the blocklist
// (loopback, link-local, RFC1918, carrier-grade NAT, cloud-metadata
// ranges, IPv6 unique-local). Unresolvable hosts fail closed.
func AllowedHost(ctx context.Context, u *url.URL) bool {
	host := u.Hostname()
	if host == "" {
		return false
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !BlockedIP(ip)
	}
	ips, err := LookupHostIPs(ctx, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if BlockedIP(ip) {
			return false
		}
	}
	return true
}

// BlockedIP reports whether ip belongs to a range the crawler must not
// contact (loopback, link-local, RFC1918, CGNAT, cloud-metadata, IPv6
// unique-local or link-local).
func BlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, cidr := range privateRanges {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// CheckRedirect is an http.Client.CheckRedirect hook that aborts the
// chain when the next target resolves to a blocked host, and caps the
// hop count at 10.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if !AllowedHost(req.Context(), req.URL) {
		return fmt.Errorf("redirect to blocked host %q", req.URL.Host)
	}
	return nil
}

var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",  // Carrier-grade NAT (RFC 6598)
		"169.254.0.0/16", // IPv4 link-local (includes 169.254.169.254 metadata)
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 ULA (includes fd00:ec2::254 AWS metadata)
		"fe80::/10",      // IPv6 link-local
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, network, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, network)
		}
	}
	return out
}()

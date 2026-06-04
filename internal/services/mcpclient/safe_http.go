package mcpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxSafeRedirects = 5

var (
	mcpDialer = &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	mcpBlockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("ff00::/8"),
	}
)

func ValidateRemoteURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("url must be a valid URL")
	}
	if strings.ToLower(u.Scheme) != "https" {
		return fmt.Errorf("url must use https")
	}
	if u.User != nil {
		return fmt.Errorf("url must not include user info")
	}
	return validateSafeHost(u.Hostname())
}

func NewSafeHTTPClient(headers map[string]string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	base := &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	var transport http.RoundTripper = &safeURLTransport{base: base}
	if len(headers) > 0 {
		transport = &headerTransport{base: transport, headers: headers}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxSafeRedirects {
				return fmt.Errorf("too many redirects")
			}
			if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
				return fmt.Errorf("redirect origin is not allowed")
			}
			return ValidateRemoteURL(req.URL.String())
		},
	}
}

type safeURLTransport struct {
	base http.RoundTripper
}

func (t *safeURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("request url is required")
	}
	if err := ValidateRemoteURL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("network %q not allowed", network)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if err := validateSafeHost(host); err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no ips for host %s", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("blocked ip %s", ip)
		}
	}
	// Pin to a validated IP so DNS cannot rebind between validation and dial.
	return mcpDialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func validateSafeHost(host string) error {
	host = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(host), "."))
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	switch host {
	case "localhost", "metadata.google.internal", "metadata", "169.254.169.254":
		return fmt.Errorf("url host is not allowed")
	}
	if strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("url host is not allowed")
		}
		return nil
	}
	if looksLikeObfuscatedIPv4(host) {
		return fmt.Errorf("url host is not allowed")
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func looksLikeObfuscatedIPv4(host string) bool {
	for _, ch := range host {
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return strings.Contains(host, ".") || host != ""
}

func isBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, prefix := range mcpBlockedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

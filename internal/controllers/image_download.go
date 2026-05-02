package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	imageDownloadMaxBytes      = 50 * 1024 * 1024
	imageDownloadMaxRedirects  = 5
	imageDownloadTotalTimeout  = 30 * time.Second
	imageDownloadHeaderTimeout = 10 * time.Second
	imageDownloadDialTimeout   = 10 * time.Second
)

// extraBlockedNets covers SSRF targets net.IP helpers don't catch:
// AWS EC2 IMDS over IPv6, 0.0.0.0/8, broadcast, and IPv4-mapped IPv6 ranges
// that map back to private/loopback space.
var extraBlockedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"255.255.255.255/32",
		"::/128",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
		"fd00:ec2::254/128",
		"::ffff:0:0/96",
		"64:ff9b::/96",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ip.IsPrivate() {
		return true
	}
	for _, n := range extraBlockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	if v4 := ip.To4(); v4 != nil && !ip.Equal(v4) {
		if isBlockedIP(v4) {
			return true
		}
	}
	return false
}

func validateRequestURL(u *url.URL) error {
	if u == nil {
		return errors.New("download image: nil url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("download image: scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("download image: empty host")
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("download image: blocked ip %s", ip)
	}
	return nil
}

var safeImageDialer = &net.Dialer{
	Timeout:   imageDownloadDialTimeout,
	KeepAlive: 30 * time.Second,
}

func safeImageDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("download image: network %q not allowed", network)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("download image: no ips for %s", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("download image: blocked ip %s", ip)
		}
	}
	// Pin the dial to a validated IP to defeat DNS rebinding between this
	// resolution and the kernel-level resolution net.Dialer would otherwise do.
	ip := ips[0]
	return safeImageDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

var safeImageHTTPClient = &http.Client{
	Timeout: imageDownloadTotalTimeout,
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           safeImageDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: imageDownloadHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	},
	// Re-validate every redirect target: an open redirect on an allowed host
	// could otherwise bounce us into 169.254.169.254 or another internal IP.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= imageDownloadMaxRedirects {
			return fmt.Errorf("download image: too many redirects")
		}
		return validateRequestURL(req.URL)
	},
}

// downloadImage fetches the image bytes at the given URL.
func downloadImage(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("download image: parse url: %w", err)
	}
	if err := validateRequestURL(parsed); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := safeImageHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, imageDownloadMaxBytes))
}

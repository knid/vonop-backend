package controllers

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"
)

// CheckPendingDomainsTask is invoked every 30s by the Asynq scheduler. For each
// app_domain in 'pending' state, resolve its DNS and flip to 'verified' if the
// records point at our edge. Domains older than 30min are marked 'failed'.
func (c *Controller) CheckPendingDomainsTask(ctx context.Context) error {
	rows, err := c.DB.GetAppDomainsPendingDNSCheck()
	if err != nil {
		return err
	}
	deadline := 30 * time.Minute

	for _, d := range rows {
		if time.Since(d.CreatedAt) > deadline {
			if mErr := c.DB.MarkAppDomainDNSStatus(d.ID.String(), "failed"); mErr != nil {
				slog.Warn("dns_mark_failed_failed", "domain", d.Domain, "err", mErr)
			}
			continue
		}

		if !dnsResolvesToEdge(d.Domain, c.EdgeIPs) {
			continue
		}

		runtimeApp, err := c.DB.GetRuntimeAppByAppProjectID(d.AppProjectID.String())
		if err != nil || runtimeApp.FlyAppName == "" {
			// DNS resolves but the app isn't deployed yet. Leave the row in
			// pending so the cron retries once the runtime row lands; the
			// 30-minute deadline above will eventually mark it failed if the
			// app is never deployed.
			slog.Warn("caddy_route_skipped_no_runtime", "domain", d.Domain, "err", err)
			continue
		}

		// Add the Caddy route before flipping the DB to verified. If Caddy
		// reload fails (e.g. a transient admin-API error), leaving the row
		// pending lets the next cron tick retry — that beats marking it
		// "verified" while the proxy is silently absent.
		if err := c.CaddyAdmin.AddCustomDomainRoute(d.Domain, runtimeApp.FlyAppName); err != nil {
			slog.Error("caddy_route_add_failed", "domain", d.Domain, "err", err)
			continue
		}

		if mErr := c.DB.MarkAppDomainDNSStatus(d.ID.String(), "verified"); mErr != nil {
			slog.Warn("dns_mark_verified_failed", "domain", d.Domain, "err", mErr)
			continue
		}
		slog.Info("dns_verified", "domain", d.Domain)
	}
	return nil
}

// dnsResolvesToEdge returns true if any A/AAAA record for `hostname` matches
// any of our known edge IPs. Returns false if EdgeIPs is empty (cron is then
// effectively a no-op until edge IPs are configured).
func dnsResolvesToEdge(hostname string, edgeIPs []string) bool {
	if len(edgeIPs) == 0 {
		return false
	}
	want := make(map[string]bool, len(edgeIPs))
	for _, ip := range edgeIPs {
		want[strings.TrimSpace(ip)] = true
	}
	addrs, err := net.LookupIP(hostname)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if want[a.String()] {
			return true
		}
	}
	return false
}

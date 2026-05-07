package controllers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/meta"
)

// MetaAttribution is the per-request bundle of ad-tracking signals we capture
// from the front-end Pixel and the originating HTTP request. Persisted on the
// user so the async Stripe webhook can fire CAPI events with browser context
// hours after the original action.
type MetaAttribution struct {
	Fbp            string // _fbp cookie set by the front-end Pixel
	Fbc            string // _fbc cookie or fb.1.<ts>.<fbclid> derived value
	EventSourceURL string // page URL the user took the action on
	ClientIP       string // X-Forwarded-For-aware client IP
	ClientUA       string // User-Agent header
}

// MetaAttributionFromRequest builds an attribution bundle from the request
// body's tracking fields plus the originating HTTP request's IP + UA.
func MetaAttributionFromRequest(r *http.Request, fbp, fbc, eventSourceURL string) MetaAttribution {
	return MetaAttribution{
		Fbp:            fbp,
		Fbc:            fbc,
		EventSourceURL: eventSourceURL,
		ClientIP:       clientIP(r),
		ClientUA:       r.UserAgent(),
	}
}

// clientIP extracts the leftmost IP from X-Forwarded-For (the originating
// client behind our Caddy/Fly edge), falling back to X-Real-IP and then
// RemoteAddr. Always returns a bare IP (no port) so the value is suitable
// for Meta CAPI's client_ip_address field, which rejects host:port forms.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr is host:port — strip the port so CAPI accepts the value.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// applyMetaAttribution copies non-empty attribution values onto the user and
// returns true when any field actually changed. Pure (no DB) so the dirty
// guard can be unit-tested independently of PersistMetaAttribution.
func applyMetaAttribution(user *models.User, attr MetaAttribution) bool {
	dirty := false
	if attr.Fbp != "" && user.MetaFbp != attr.Fbp {
		user.MetaFbp = attr.Fbp
		dirty = true
	}
	if attr.Fbc != "" && user.MetaFbc != attr.Fbc {
		user.MetaFbc = attr.Fbc
		dirty = true
	}
	if attr.ClientIP != "" && user.MetaClientIP != attr.ClientIP {
		user.MetaClientIP = attr.ClientIP
		dirty = true
	}
	if attr.ClientUA != "" && user.MetaClientUA != attr.ClientUA {
		user.MetaClientUA = attr.ClientUA
		dirty = true
	}
	if attr.EventSourceURL != "" && user.MetaEventSourceURL != attr.EventSourceURL {
		user.MetaEventSourceURL = attr.EventSourceURL
		dirty = true
	}
	return dirty
}

// PersistMetaAttribution writes the attribution bundle onto the user row.
// Skips empty fields so partial updates (e.g. checkout pageload that only
// has updated fbp/fbc) don't clobber the register-time IP/UA, and skips the
// DB write entirely when no field changed.
func (c *Controller) PersistMetaAttribution(user *models.User, attr MetaAttribution) {
	if user == nil {
		return
	}
	if !applyMetaAttribution(user, attr) {
		return
	}
	if _, err := c.DB.UpdateUser(user); err != nil {
		slog.Warn("meta: failed to persist attribution to user", "error", err, "user_id", user.ID)
	}
}

// userMetaUserData builds the CAPI user_data block for a user, preferring the
// passed attribution (request-time) over the user's persisted values, then
// falling back to the user record.
func (c *Controller) userMetaUserData(user *models.User, attr MetaAttribution) meta.UserData {
	pick := func(req, stored string) string {
		if req != "" {
			return req
		}
		return stored
	}
	ud := meta.UserData{
		ClientIP: pick(attr.ClientIP, user.MetaClientIP),
		ClientUA: pick(attr.ClientUA, user.MetaClientUA),
		Fbp:      pick(attr.Fbp, user.MetaFbp),
		Fbc:      pick(attr.Fbc, user.MetaFbc),
	}
	if h := meta.HashEmail(user.Email); h != "" {
		ud.Em = []string{h}
	}
	if h := meta.Hash(user.ID.String()); h != "" {
		ud.ExternalID = []string{h}
	}
	if user.Name != "" {
		// First/last name: lowercased SHA-256 per Meta normalization spec.
		// Wemob stores a single Name field; treat it as first name and split
		// off any trailing words as last name.
		parts := strings.Fields(user.Name)
		if len(parts) > 0 {
			if h := meta.HashLowercase(parts[0]); h != "" {
				ud.Fn = []string{h}
			}
		}
		if len(parts) > 1 {
			if h := meta.HashLowercase(strings.Join(parts[1:], " ")); h != "" {
				ud.Ln = []string{h}
			}
		}
	}
	return ud
}

// firePixelEvent is the shared sender. Returns immediately when CAPI is
// disabled. Run in a goroutine at call sites — Send has its own 10s timeout.
func (c *Controller) firePixelEvent(eventName, eventID string, user *models.User, attr MetaAttribution, custom meta.CustomData) {
	if c.MetaCAPI == nil || user == nil {
		return
	}
	eventSourceURL := attr.EventSourceURL
	if eventSourceURL == "" {
		eventSourceURL = user.MetaEventSourceURL
	}
	ev := meta.Event{
		EventName:      eventName,
		EventTime:      time.Now().Unix(),
		EventID:        eventID,
		EventSourceURL: eventSourceURL,
		ActionSource:   "website",
		UserData:       c.userMetaUserData(user, attr),
		CustomData:     custom,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_ = c.MetaCAPI.Send(ctx, ev)
}

// FireMetaCompleteRegistration sends Meta's CompleteRegistration event.
// EventID is derived from the user UUID so a webhook retry or a duplicate
// register call can't double-count. We don't pass a value/currency pair —
// Meta requires both or neither, and signup has no monetary value to attach.
func (c *Controller) FireMetaCompleteRegistration(user *models.User, attr MetaAttribution) {
	if c.MetaCAPI == nil || user == nil {
		return
	}
	c.firePixelEvent("CompleteRegistration", "register-"+user.ID.String(), user, attr, meta.CustomData{
		ContentName: "Wemob Account",
	})
}

// FireMetaInitiateCheckout sends Meta's InitiateCheckout event, fired when a
// Stripe Checkout Session is created (subscription or credit-pack flow).
// EventID uses the Stripe session ID to dedup against retries.
func (c *Controller) FireMetaInitiateCheckout(user *models.User, attr MetaAttribution, sessionID, contentName string, value float64, currency string) {
	if c.MetaCAPI == nil || user == nil {
		return
	}
	custom := meta.CustomData{
		Value:       value,
		Currency:    strings.ToUpper(currency),
		ContentName: contentName,
		ContentIDs:  []string{sessionID},
		ContentType: "product",
		OrderID:     sessionID,
	}
	c.firePixelEvent("InitiateCheckout", "initiate-"+sessionID, user, attr, custom)
}

// FireMetaSubscribe sends Meta's Subscribe event from the Stripe webhook on
// initial subscription create. Use the persisted user attribution.
func (c *Controller) FireMetaSubscribe(user *models.User, subscriptionID, planName string, value float64, currency string) {
	if c.MetaCAPI == nil || user == nil {
		return
	}
	custom := meta.CustomData{
		Value:          value,
		Currency:       strings.ToUpper(currency),
		ContentName:    planName,
		ContentIDs:     []string{subscriptionID},
		ContentType:    "subscription",
		SubscriptionID: subscriptionID,
		OrderID:        subscriptionID,
	}
	c.firePixelEvent("Subscribe", "subscribe-"+subscriptionID, user, MetaAttribution{}, custom)
}

// FireMetaPurchase sends Meta's Purchase event. Used for every paid invoice
// (first + renewals) and for one-shot credit-pack purchases. orderID must be
// stable across webhook retries — Stripe invoice ID for subscriptions,
// checkout session ID for credit packs.
func (c *Controller) FireMetaPurchase(user *models.User, orderID, contentName string, contentIDs []string, value float64, currency, contentType string) {
	if c.MetaCAPI == nil || user == nil {
		return
	}
	custom := meta.CustomData{
		Value:       value,
		Currency:    strings.ToUpper(currency),
		ContentName: contentName,
		ContentIDs:  contentIDs,
		ContentType: contentType,
		OrderID:     orderID,
	}
	c.firePixelEvent("Purchase", "purchase-"+orderID, user, MetaAttribution{}, custom)
}

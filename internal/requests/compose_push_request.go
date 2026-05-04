package requests

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// ComposePushRequest is the dashboard's "send a push" payload for a mobile_app_native project.
//
// AudienceType is one of "segments" or "external_ids". Only one set of audience
// IDs is read per request:
//   - "segments": audience_segments populated; defaults to ["Subscribed Users"] if empty.
//   - "external_ids": audience_external_user_ids populated (Better-Auth user IDs from P2.4 wiring).
//
// Title and Body are localised maps keyed by IETF tag. The dashboard ships
// at minimum {en: "..."}; agents that translate user content can add more.
// Validators: title ≤ 65 chars (OneSignal display cap on iOS lock screen),
// body ≤ 240 chars (Apple's lock-screen line-wrap cap).
//
// <P2#25> additions:
//   - SendAfter: schedule the notification at a future time (RFC 3339).
//   - BigPicture / IOSAttachmentURL: Android big-picture + iOS rich media.
//   - WebURL / AppURL: deep links — sanitised against an http(s)+scheme allow-list.
//   - Localizations: per-locale {title, body} pairs flattened into Title+Body.
type ComposePushRequest struct {
	AudienceType            string            `json:"audience_type" validate:"required,oneof=segments external_ids" errormgs:"audience_type must be segments or external_ids"`
	AudienceSegments        []string          `json:"audience_segments,omitempty" validate:"omitempty,max=10,dive,min=1,max=64"`
	AudienceExternalUserIDs []string          `json:"audience_external_user_ids,omitempty" validate:"omitempty,max=2000,dive,uuid4"`
	Title                   map[string]string `json:"title" validate:"required,min=1,dive,keys,min=2,max=8,endkeys,min=1,max=65"`
	Body                    map[string]string `json:"body" validate:"required,min=1,dive,keys,min=2,max=8,endkeys,min=1,max=240"`

	// <P2#25> Scheduled send. Empty = "now". Must be RFC 3339 / ISO 8601
	// when set; controller defends with isValidRFC3339Future.
	SendAfter string `json:"send_after,omitempty" validate:"omitempty"`

	// <P2#25> Rich media. BigPicture is the Android big-picture URL.
	// IOSAttachmentURL is forwarded as {"id": <url>} to OneSignal's
	// ios_attachments field. Both must pass isSafeMediaURL (http/https only).
	BigPicture       string `json:"big_picture,omitempty" validate:"omitempty,url"`
	IOSAttachmentURL string `json:"ios_attachment_url,omitempty" validate:"omitempty,url"`

	// <P2#25> Deep link — sanitised in Validate() via isSafeNotificationURL.
	// WebURL is followed when the user taps a notification on the web; AppURL
	// is the universal-link / custom-scheme URL when running inside the
	// native app. URLScheme is the project's allowed custom scheme (e.g.
	// "myapp"); reject everything else.
	WebURL     string `json:"web_url,omitempty"`
	AppURL     string `json:"app_url,omitempty"`
	URLScheme  string `json:"url_scheme,omitempty" validate:"omitempty,max=64"`

	// <P2#25> Localizations: per-locale {title, body} pairs. Folded into
	// Title + Body inside Validate() so downstream code only deals with
	// the maps. Any locale already present in Title/Body wins.
	Localizations map[string]LocalizedPushContent `json:"localizations,omitempty" validate:"omitempty,dive,keys,min=2,max=8,endkeys"`
}

// LocalizedPushContent is one entry of ComposePushRequest.Localizations.
type LocalizedPushContent struct {
	Title string `json:"title" validate:"required,min=1,max=65"`
	Body  string `json:"body" validate:"required,min=1,max=240"`
}

// Validate runs the struct validator over r, then folds Localizations into
// Title+Body and applies the URL allow-list.
func (r *ComposePushRequest) Validate(v *validator.Validate) error {
	if err := v.Struct(r); err != nil {
		return err
	}
	// Fold per-locale entries into Title/Body. Existing keys in Title/Body
	// win over Localizations (the dashboard is the canonical source).
	if len(r.Localizations) > 0 {
		if r.Title == nil {
			r.Title = map[string]string{}
		}
		if r.Body == nil {
			r.Body = map[string]string{}
		}
		for locale, lc := range r.Localizations {
			if _, ok := r.Title[locale]; !ok {
				r.Title[locale] = lc.Title
			}
			if _, ok := r.Body[locale]; !ok {
				r.Body[locale] = lc.Body
			}
		}
	}
	if r.SendAfter != "" {
		if _, perr := time.Parse(time.RFC3339, r.SendAfter); perr != nil {
			return errors.New("send_after must be RFC 3339 (e.g. 2026-05-04T18:30:00Z)")
		}
	}
	if r.WebURL != "" {
		if !isSafeNotificationURL(r.WebURL, r.URLScheme) {
			return errors.New("web_url scheme not allowed (must be http/https)")
		}
	}
	if r.AppURL != "" {
		if !isSafeNotificationURL(r.AppURL, r.URLScheme) {
			return errors.New("app_url scheme not allowed (http/https or project URL scheme only)")
		}
	}
	if r.BigPicture != "" && !isSafeMediaURL(r.BigPicture) {
		return errors.New("big_picture must be http(s)")
	}
	if r.IOSAttachmentURL != "" && !isSafeMediaURL(r.IOSAttachmentURL) {
		return errors.New("ios_attachment_url must be http(s)")
	}
	return nil
}

// isSafeNotificationURL accepts http/https plus an optional project-specific
// custom scheme (e.g. "myapp"). Rejects javascript:/file:/data:/etc.
func isSafeNotificationURL(raw, projectURLScheme string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		return true
	}
	if projectURLScheme != "" && scheme == strings.ToLower(projectURLScheme) {
		return true
	}
	return false
}

// isSafeMediaURL is stricter than isSafeNotificationURL — only http/https.
// Used for media attachments where javascript: / file: / data: would either
// fail to render or open an attack surface.
func isSafeMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

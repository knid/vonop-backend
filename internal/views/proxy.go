package views

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultWidth     = 375
	DefaultScale     = 0.25
	MaxInlineJSBytes = 100 * 1024
	ServerUserAgent  = "Wemob-Viewport-Proxy/1.6"
	ListenAddr       = ":8080"
	ProxyBase        = "https://7a1d46d1052b.ngrok-free.app/v1/proxy?url=" // 👈 kendi domainini yaz
)

func (v *HttpViews) HandleProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing ?url=", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || !u.IsAbs() {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	width := intFromQuery(r, "width", DefaultWidth)
	scale := floatFromQuery(r, "scale", DefaultScale)

	injectJSUrl := r.URL.Query().Get("inject_js_url")
	injectJSBase64 := r.URL.Query().Get("inject_js_base64")

	var inlineJS string
	if injectJSBase64 != "" {
		decoded, decErr := decodeBase64Flexible(injectJSBase64, MaxInlineJSBytes)
		if decErr != nil {
			http.Error(w, fmt.Sprintf("invalid inject_js_base64: %v", decErr), http.StatusBadRequest)
			return
		}
		inlineJS = decoded
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("User-Agent", ServerUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	h := cloneHeaders(resp.Header)
	h.Del("Content-Length")
	h.Del("X-Frame-Options")
	h.Del("Content-Security-Policy")

	bodyBytes, _ := io.ReadAll(resp.Body)
	ct := resp.Header.Get("Content-Type")

	if strings.Contains(strings.ToLower(ct), "text/html") {
		html := string(bodyBytes)
		html = injectViewportString(html, width, scale)
		html = injectProxyBaseTag(html, u) // 👈 sadece burası eklendi
		html = injectJSToHTML(html, inlineJS, injectJSUrl)
		bodyBytes = []byte(html)
		h.Set("Content-Type", "text/html; charset=utf-8")
	}

	for k, vs := range h {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(bodyBytes)
}

/* ---------- Proxy-tabanlı base href ---------- */

func injectProxyBaseTag(html string, targetURL *url.URL) string {
	reBase := regexp.MustCompile(`(?is)<base[^>]+href\s*=\s*["'][^"']*["'][^>]*>`)
	// hedef siteyi encode et
	baseHref := fmt.Sprintf("%s%s://%s/", ProxyBase, targetURL.Scheme, targetURL.Host)
	baseTag := fmt.Sprintf(`<base href="%s">`, baseHref)

	if reBase.MatchString(html) {
		return reBase.ReplaceAllString(html, baseTag)
	}

	reHead := regexp.MustCompile(`(?i)<head[^>]*>`)
	if reHead.MatchString(html) {
		return reHead.ReplaceAllString(html, "${0}\n"+baseTag)
	}

	return baseTag + "\n" + html
}

/* ---------- Viewport injection ---------- */

var (
	reViewport  = regexp.MustCompile(`(?is)<meta[^>]+name\s*=\s*["']viewport["'][^>]*>`)
	reHeadOpen  = regexp.MustCompile(`(?i)<head[^>]*>`)
	reBodyClose = regexp.MustCompile(`(?i)</body\s*>`)
)

func injectViewportString(html string, width int, scale float64) string {
	meta := fmt.Sprintf(`<meta name="viewport" content="width=%d, initial-scale=%g, user-scalable=no">`, width, scale)
	if reViewport.MatchString(html) {
		html = reViewport.ReplaceAllString(html, meta)
	} else if reHeadOpen.MatchString(html) {
		html = reHeadOpen.ReplaceAllString(html, "${0}\n"+meta)
	} else {
		html = meta + "\n" + html
	}
	return html
}

/* ---------- JS injection ---------- */

func injectJSToHTML(html string, inlineJS string, jsURL string) string {
	var scriptTag string
	if jsURL != "" {
		scriptTag = fmt.Sprintf(`<script src="%s" defer></script>`, htmlEscapeAttr(jsURL))
	} else if inlineJS != "" {
		scriptTag = "<script>\n" + inlineJS + "\n</script>"
	} else {
		return html
	}

	if reBodyClose.MatchString(html) {
		return reBodyClose.ReplaceAllString(html, scriptTag+"$0")
	}
	if reHeadOpen.MatchString(html) {
		return reHeadOpen.ReplaceAllString(html, "${0}\n"+scriptTag)
	}
	return scriptTag + "\n" + html
}

/* ---------- Helpers ---------- */

func htmlEscapeAttr(s string) string {
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func decodeBase64Flexible(b64 string, maxBytes int) (string, error) {
	s := strings.TrimSpace(b64)
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if strings.Contains(s, " ") {
		s = strings.ReplaceAll(s, " ", "+")
	}

	addPadding := func(in string) string {
		pad := (4 - (len(in) % 4)) % 4
		if pad == 0 {
			return in
		}
		return in + strings.Repeat("=", pad)
	}

	try := func(dec func(string) ([]byte, error), input string) (string, error) {
		if b, err := dec(input); err == nil {
			if len(b) > maxBytes {
				return "", fmt.Errorf("decoded JS too large (%d bytes, max %d)", len(b), maxBytes)
			}
			return string(b), nil
		}
		return "", fmt.Errorf("decode failed")
	}

	if out, err := try(base64.RawURLEncoding.DecodeString, s); err == nil {
		return out, nil
	}
	if out, err := try(base64.URLEncoding.DecodeString, addPadding(s)); err == nil {
		return out, nil
	}
	if out, err := try(base64.RawStdEncoding.DecodeString, s); err == nil {
		return out, nil
	}
	if out, err := try(base64.StdEncoding.DecodeString, addPadding(s)); err == nil {
		return out, nil
	}
	return "", fmt.Errorf("base64 decode failed")
}

func cloneHeaders(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for k, vs := range src {
		cp := make([]string, len(vs))
		copy(cp, vs)
		dst[k] = cp
	}
	return dst
}

func intFromQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return v
	}
	return def
}

func floatFromQuery(r *http.Request, key string, def float64) float64 {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
		return v
	}
	return def
}

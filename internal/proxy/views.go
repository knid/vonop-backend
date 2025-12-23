package proxy

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/knid/wemob/internal/constants"
)

var customTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          1000,
	MaxIdleConnsPerHost:   100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
}

func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("🔹 [START] Request received for: %s", r.URL.Path)

	UpstreamProxyHost := os.Getenv("PREVIEW_PROXY_HOST")
	UpstreamProxyAuth := os.Getenv("PREVIEW_PROXY_AUTH")

	if UpstreamProxyHost != "" {
		upstreamUrl := &url.URL{Scheme: "http", Host: UpstreamProxyHost}
		if UpstreamProxyAuth != "" {
			parts := strings.Split(UpstreamProxyAuth, ":")
			if len(parts) == 2 {
				upstreamUrl.User = url.UserPassword(parts[0], parts[1])
			}
		}
		customTransport.Proxy = http.ProxyURL(upstreamUrl)
	}

	queryParam := r.URL.Query().Get("url")
	if queryParam != "" {
		targetURL, err := url.Parse(queryParam)
		if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
			http.Error(w, "Invalid 'url'", http.StatusBadRequest)
			return
		}

		isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		secureMode := false
		sameSiteMode := http.SameSiteLaxMode

		if isSecure {
			secureMode = true
			sameSiteMode = http.SameSiteNoneMode
		}

		http.SetCookie(w, &http.Cookie{
			Name:     constants.SessionCookieName,
			Value:    targetURL.String(),
			Path:     "/",
			HttpOnly: true,
			Secure:   secureMode,
			SameSite: sameSiteMode,
			Expires:  time.Now().Add(24 * time.Hour),
		})

		log.Printf("✅ [SESSION] New session for: %s", targetURL.String())
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	cookie, err := r.Cookie(constants.SessionCookieName)
	if err != nil || cookie.Value == "" {
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Printf("⚠️ [AUTH] No active session cookie found")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<h3>No Active Session</h3>`)
		return
	}

	targetBase := cookie.Value
	targetURL, err := url.Parse(targetBase)
	if err != nil {
		log.Printf("❌ [ERROR] Invalid target URL in cookie: %v", err)
		http.Error(w, "Invalid session target", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = customTransport

	proxy.ErrorLog = log.New(os.Stderr, "🔥 [PROXY-INTERNAL] ", log.LstdFlags)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host

		req.Header.Del("Accept-Encoding")
		req.Header.Del("X-Forwarded-For")

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", targetURL.String())
		req.Header.Set("Origin", targetURL.Scheme+"://"+targetURL.Host)

		log.Printf("📡 [UPSTREAM] Sending request to: %s", req.URL.String())
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		log.Printf("🔙 [RESPONSE] Got status %d from upstream. Type: %s", resp.StatusCode, resp.Header.Get("Content-Type"))

		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Strict-Transport-Security")
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Credentials", "true")

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc != "" {
				u, err := url.Parse(loc)
				if err == nil && (u.Host == targetURL.Host || u.Host == "") {
					newPath := u.Path
					if u.RawQuery != "" {
						newPath += "?" + u.RawQuery
					}
					resp.Header.Set("Location", newPath)
					log.Printf("🔀 [REDIRECT] Rewrote location to: %s", newPath)
				}
			}
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/html") {
			log.Printf("📝 [REWRITE] Starting HTML rewrite...")
			err := rewriteHTML(resp, targetURL)
			if err != nil {
				log.Printf("❌ [REWRITE-ERROR] Failed to rewrite HTML: %v", err)
			} else {
				log.Printf("✅ [REWRITE] HTML rewrite success")
			}
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("❌ [HANDLER-ERROR] Proxy Error for %s: %v", r.URL.Path, err)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, "Proxy Gateway Error: %v", err)
	}

	proxy.ServeHTTP(w, r)
}

func rewriteHTML(resp *http.Response, targetURL *url.URL) error {
	if resp.Body == nil {
		return fmt.Errorf("response body is nil")
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body failed: %v", err)
	}
	resp.Body.Close()

	encoding := resp.Header.Get("Content-Encoding")
	if strings.Contains(encoding, "gzip") {
		log.Printf("📦 [GZIP] Decompressing Gzip content...")
		gr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
		if err != nil {
			log.Printf("⚠️ [GZIP-FAIL] Failed to create gzip reader: %v. Using raw body.", err)
		} else {
			decompressedBytes, err := io.ReadAll(gr)
			gr.Close()
			if err != nil {
				log.Printf("⚠️ [GZIP-READ-FAIL] Failed to read gzip stream: %v", err)
			} else {
				bodyBytes = decompressedBytes
				resp.Header.Del("Content-Encoding")
			}
		}
	} else if encoding != "" {
		log.Printf("⚠️ [UNKNOWN-ENCODING] Content-Encoding is '%s'. Skipping rewrite to avoid corruption.", encoding)
		resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		return nil
	}

	htmlContent := string(bodyBytes)

	reBase := regexp.MustCompile(`(?i)<base[^>]*>`)
	htmlContent = reBase.ReplaceAllString(htmlContent, "")

	targetString := targetURL.String()
	targetStringNoSlash := strings.TrimRight(targetString, "/")
	htmlContent = strings.ReplaceAll(htmlContent, targetString, "")
	htmlContent = strings.ReplaceAll(htmlContent, targetStringNoSlash, "")

	reIntegrity := regexp.MustCompile(`\s*integrity=["'][^"']*["']`)
	htmlContent = reIntegrity.ReplaceAllString(htmlContent, "")

	buf := bytes.NewBufferString(htmlContent)
	resp.Body = io.NopCloser(buf)

	resp.ContentLength = -1
	resp.Header.Del("Content-Length")

	return nil
}

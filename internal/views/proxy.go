package views

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

// InitProxyTransport configures the upstream proxy once at startup.
// Must be called before any requests hit ProxyHandler.
func InitProxyTransport(host, auth string) {
	if host == "" {
		return
	}
	upstreamURL := &url.URL{Scheme: "http", Host: host}
	if auth != "" {
		parts := strings.Split(auth, ":")
		if len(parts) == 2 {
			upstreamURL.User = url.UserPassword(parts[0], parts[1])
		}
	}
	customTransport.Proxy = http.ProxyURL(upstreamURL)
}

func (v *HttpViews) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	// tokenString, err := utils.ExtractTokenFromHeader(r)
	// if err != nil {
	// slog.Error("Failed to extract token", "error", err, "from", "PROXY")
	// http.Error(w, "Unauthorized: Failed to extract token", http.StatusUnauthorized)
	// return
	// }

	// userInformationRes, err := v.Controller.GetUser(tokenString)
	// if err != nil {
	// slog.Error("Failed to get user information", "error", err, "user.token", tokenString[0:12]+"...", "from", "PROXY")
	// http.Error(w, "Unauthorized: Failed to get user information", http.StatusUnauthorized)
	// return
	// }

	/* slog.Info(fmt.Sprintf("Proxy start request received for: %s", r.URL.Path),
	"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
	"user.email", userInformationRes.Email, "from", "PROXY") */

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

		/* slog.Info(fmt.Sprintf("New session for: %s", targetURL.String()),
		"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
		"user.email", userInformationRes.Email, "from", "PROXY") */
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	cookie, err := r.Cookie(constants.SessionCookieName)
	if err != nil || cookie.Value == "" {
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		/* slog.Error("No active session cookie found",
		"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
		"user.email", userInformationRes.Email, "from", "PROXY") */
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<h3>No Active Session</h3>`)
		return
	}

	targetBase := cookie.Value
	targetURL, err := url.Parse(targetBase)
	if err != nil {
		/* slog.Error(fmt.Sprintf("Invalid target URL in cookie: %v", err),
		"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
		"user.email", userInformationRes.Email, "from", "PROXY") */
		http.Error(w, "Invalid session target", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = customTransport

	// proxy.ErrorLog = log.New(os.Stderr, "🔥 [PROXY-INTERNAL] ", log.LstdFlags)

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

		/* slog.Info(fmt.Sprintf("[UPSTREAM] Sending request to: %s", req.URL.String()),
		"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
		"user.email", userInformationRes.Email, "from", "PROXY") */
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

					/* slog.Info(fmt.Sprintf("[REDIRECT] Rewrote location to: %s", newPath),
					"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
					"user.email", userInformationRes.Email, "from", "PROXY") */
				}
			}
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/html") {
			/* slog.Info("Starting HTML rewrite...",
			"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
			"user.email", userInformationRes.Email, "from", "PROXY") */

			err := rewriteHTML(resp, targetURL)
			if err != nil {
				/* slog.Error(fmt.Sprintf("Failed to rewrite HTML: %v", err),
				"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
				"user.email", userInformationRes.Email, "from", "PROXY") */
			} else {
				/* slog.Info("HTML rewrite success",
				"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
				"user.email", userInformationRes.Email, "from", "PROXY") */
			}
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		/* slog.Error(fmt.Sprintf("Proxy Error for %s: %v", r.URL.Path, err),
		"user.token", tokenString[0:12]+"...", "user.id", userInformationRes.ID.String(),
		"user.email", userInformationRes.Email, "from", "PROXY") */
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, "Proxy Gateway Error: %v", err)
	}

	proxy.ServeHTTP(w, r)
}

func rewriteHTML(resp *http.Response, targetURL *url.URL) error {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()

	encoding := resp.Header.Get("Content-Encoding")
	if strings.Contains(encoding, "gzip") {
		gr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
		if err == nil {
			bodyBytes, _ = io.ReadAll(gr)
			gr.Close()
			resp.Header.Del("Content-Encoding")
		}
	} else if encoding != "" {
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return nil
	}

	htmlContent := string(bodyBytes)

	reBase := regexp.MustCompile(`(?i)<base[^>]*>`)
	htmlContent = reBase.ReplaceAllString(htmlContent, "")

	reIntegrity := regexp.MustCompile(`\s*integrity=["'][^"']*["']`)
	htmlContent = reIntegrity.ReplaceAllString(htmlContent, "")

	proxyScript := fmt.Sprintf(`
	<script>
		(function() {
			const TARGET_ORIGIN = '%s://%s';
			
			function wrapUrl(u) {
				if (!u) return u;
				if (u.includes('?url=')) return u;
				if (u.startsWith('#') || u.startsWith('javascript:') || u.startsWith('mailto:')) return u;

				try {
					let fullUrl = new URL(u, window.location.href).href;
					
					if (fullUrl.startsWith(TARGET_ORIGIN)) {
						let path = fullUrl.replace(TARGET_ORIGIN, '');
						if (path === '') path = '/';
						return path;
					}
				} catch (e) {}
				return u;
			}

			document.addEventListener('click', function(e) {
				const link = e.target.closest('a, area');
				if (link) {
					const href = link.getAttribute('href');
					if (href) {
						if (link.target === '_blank') link.target = '_self';

						const newUrl = wrapUrl(href);
						if (newUrl !== href && !href.startsWith('/')) {
							e.preventDefault();
							e.stopPropagation();
							window.location.assign(newUrl);
						}
					}
				}
			}, true);

			document.addEventListener('submit', function(e) {
				const form = e.target;
				const action = form.getAttribute('action');
				if (action) {
					const newAction = wrapUrl(action);
					if (newAction !== action) {
						form.setAttribute('action', newAction);
					}
				}
			}, true);
            
            const originalPushState = history.pushState;
            history.pushState = function(state, unused, url) {
                if (url && typeof url === 'string' && url.startsWith(TARGET_ORIGIN)) {
                    url = url.replace(TARGET_ORIGIN, '');
                }
                return originalPushState.apply(this, [state, unused, url]);
            };

            const originalReplaceState = history.replaceState;
            history.replaceState = function(state, unused, url) {
                if (url && typeof url === 'string' && url.startsWith(TARGET_ORIGIN)) {
                    url = url.replace(TARGET_ORIGIN, '');
                }
                return originalReplaceState.apply(this, [state, unused, url]);
            };
		})();
	</script>
	`, targetURL.Scheme, targetURL.Host)

	proxyStyle := `
	<style>
		html, body {
			height: 100%;
			overflow: auto;
			-webkit-overflow-scrolling: touch;
		}

		html, body {
			scrollbar-width: none;
		}

		html::-webkit-scrollbar, body::-webkit-scrollbar {
			width: 0;
			height: 0;
		}
	</style>
	`

	if strings.Contains(htmlContent, "</head>") {
		htmlContent = strings.Replace(htmlContent, "</head>", proxyScript+proxyStyle+"</head>", 1)
	} else {
		htmlContent = proxyScript + proxyStyle + htmlContent
	}

	buf := bytes.NewBufferString(htmlContent)
	resp.Body = io.NopCloser(buf)

	resp.ContentLength = -1
	resp.Header.Del("Content-Length")

	return nil
}

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
	// 1. Body Okuma ve Gzip Açma (Burası aynı kalıyor, çünkü içeriği okumamız lazım)
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
		// Tanımadığımız sıkıştırma varsa dokunmuyoruz
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return nil
	}

	htmlContent := string(bodyBytes)

	// ---------------------------------------------------------
	// DEĞİŞİKLİK BURADA: Artık Domainleri "ReplaceAll" ile silmiyoruz!
	// Bu işlem React'ın JSON verisini bozup siteyi çökertiyor.
	// Bunun yerine <head> içine bir Script enjekte ediyoruz.
	// ---------------------------------------------------------

	// 1. Base Tag Varsa Sil (Bu hala gerekli)
	reBase := regexp.MustCompile(`(?i)<base[^>]*>`)
	htmlContent = reBase.ReplaceAllString(htmlContent, "")

	// 2. Integrity Korumasını Kaldır (Script ekleyeceğimiz için hash tutmaz)
	reIntegrity := regexp.MustCompile(`\s*integrity=["'][^"']*["']`)
	htmlContent = reIntegrity.ReplaceAllString(htmlContent, "")

	// 3. JAVASCRIPT ENJEKSİYONU (Navigasyon Kontrolcüsü)
	// Bu script, kullanıcı bir linke tıkladığında devreye girer.
	// React'ın çalışmasını bozmaz ama kullanıcıyı proxy'de tutar.

	proxyScript := fmt.Sprintf(`
	<script>
		(function() {
			const TARGET_ORIGIN = '%s://%s';
			
			// Linkleri Proxy formatına çeviren fonksiyon
			function wrapUrl(u) {
				if (!u) return u;
				if (u.includes('?url=')) return u;
				if (u.startsWith('#') || u.startsWith('javascript:') || u.startsWith('mailto:')) return u;

				try {
					let fullUrl = new URL(u, window.location.href).href;
					
					// Eğer link hedef siteye gidiyorsa (https://gumastore.com/checkout)
					if (fullUrl.startsWith(TARGET_ORIGIN)) {
						// KRİTİK DÜZELTME:
						// Eskiden: return '/?url=' + encodeURIComponent(fullUrl); 
						// Bu, her tıklamada session'ı sıfırlayıp ana sayfaya atıyordu.
						
						// Yeni: Domain kısmını sil, sadece path kalsın.
						// Örnek: https://gumastore.com/checkout -> /checkout
						let path = fullUrl.replace(TARGET_ORIGIN, '');
						if (path === '') path = '/';
						return path;
					}
				} catch (e) {}
				return u;
			}

			// 1. Tıklamaları Yakala
			document.addEventListener('click', function(e) {
				const link = e.target.closest('a, area');
				if (link) {
					const href = link.getAttribute('href');
					if (href) {
						// Target="_blank" varsa onu iptal et, proxy içinde kalsın
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

			// 2. Form Gönderimlerini Yakala
			document.addEventListener('submit', function(e) {
				const form = e.target;
				const action = form.getAttribute('action');
				if (action) {
					const newAction = wrapUrl(action);
					if (newAction !== action) {
						// Form action'ı fiziksel olarak değiştir
						form.setAttribute('action', newAction);
					}
				}
			}, true);
            
            // 3. History API Patch (React/Next.js Router'ın kafası karışmasın diye)
            // Eğer uygulama history.pushState ile tam domainli URL iterse, onu temizliyoruz.
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

			console.log('WeMob Proxy: Navigation Loop Fix Loaded');
		})();
	</script>
	`, targetURL.Scheme, targetURL.Host)

	// Scripti <head> bitiminden hemen önceye ekle
	if strings.Contains(htmlContent, "</head>") {
		htmlContent = strings.Replace(htmlContent, "</head>", proxyScript+"</head>", 1)
	} else {
		// Head yoksa body başına ekle
		htmlContent = proxyScript + htmlContent
	}

	// ---------------------------------------------------------

	buf := bytes.NewBufferString(htmlContent)
	resp.Body = io.NopCloser(buf)

	// Chunked transfer
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")

	return nil
}

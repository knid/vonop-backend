package views

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func (v *HttpViews) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	// 0. API Koruması (Kendi endpointlerin)
	myApiRoutes := []string{"/users", "/projects", "/api", "/auth", "/health"}
	for _, route := range myApiRoutes {
		if strings.HasPrefix(r.URL.Path, route) {
			http.NotFound(w, r)
			return
		}
	}

	// 1. Parametre Kontrolü
	targetParam := r.URL.Query().Get("url")
	if targetParam == "" {
		cookie, err := r.Cookie("wemob_target")
		if err == nil && cookie.Value != "" {
			targetParam = cookie.Value
		} else {
			http.Error(w, "url param is required", http.StatusBadRequest)
			return
		}
	}

	targetURL, err := url.Parse(targetParam)
	if err != nil {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	if targetURL.Scheme == "" || targetURL.Host == "" {
		cookie, err := r.Cookie("wemob_target")
		if err == nil && cookie.Value != "" {
			if baseURL, err := url.Parse(cookie.Value); err == nil {
				targetURL = baseURL.ResolveReference(targetURL)
			}
		}
	}

	// 2. Cookie Kaydet
	http.SetCookie(w, &http.Cookie{
		Name:     "wemob_target",
		Value:    targetURL.String(),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})

	// 3. Proxy Ayarları
	proxyURLString := fmt.Sprintf("http://%s@%s", os.Getenv("PREVIEW_PROXY_AUTH"), os.Getenv("PREVIEW_PROXY_HOST"))
	proxyURL, _ := url.Parse(proxyURLString)

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	if proxyURL != nil {
		proxy.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.URL.Path = targetURL.Path
		req.URL.RawQuery = targetURL.RawQuery
		req.RequestURI = ""

		req.Header.Del("Accept-Encoding")
		req.Header.Del("Cookie")
		req.Header.Del("X-Forwarded-For")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode < 200 {
			return nil
		}

		headersToDelete := []string{"X-Frame-Options", "Content-Security-Policy", "Content-Security-Policy-Report-Only", "Frame-Options"}
		for _, h := range headersToDelete {
			resp.Header.Del(h)
		}
		resp.Header.Set("Access-Control-Allow-Origin", "*")

		// 301/302 Redirect Yakalama ve Düzeltme
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			if loc, err := resp.Location(); err == nil {
				absLoc := targetURL.ResolveReference(loc)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				newLoc := fmt.Sprintf("%s://%s/v1/proxy?url=%s", scheme, r.Host, url.QueryEscape(absLoc.String()))
				resp.Header.Set("Location", newLoc)
			}
			return nil
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))

		// --- Helper: URL Wrapper ---
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		myProxyBase := fmt.Sprintf("%s://%s/v1/proxy?url=", scheme, r.Host)

		wrapURL := func(original string) string {
			clean := strings.TrimSpace(original)
			if strings.Contains(clean, " ") || strings.Contains(clean, ",") {
				return original
			}
			if clean == "" || strings.HasPrefix(clean, "data:") || strings.HasPrefix(clean, "#") ||
				strings.HasPrefix(clean, "javascript:") || strings.Contains(clean, "/v1/proxy") {
				return original
			}
			u, err := url.Parse(clean)
			if err != nil {
				return original
			}
			absURL := targetURL.ResolveReference(u).String()
			return myProxyBase + url.QueryEscape(absURL)
		}

		// CSS
		if strings.Contains(contentType, "text/css") {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cssContent := string(bodyBytes)
			reCSS := regexp.MustCompile(`url\(\s*['"]?([^'"\)]+)['"]?\s*\)`)
			cssContent = reCSS.ReplaceAllStringFunc(cssContent, func(match string) string {
				sub := reCSS.FindStringSubmatch(match)
				if len(sub) < 2 {
					return match
				}
				return fmt.Sprintf("url('%s')", wrapURL(sub[1]))
			})
			buf := bytes.NewBufferString(cssContent)
			resp.Body = io.NopCloser(buf)
			resp.ContentLength = int64(buf.Len())
			resp.Header.Set("Content-Length", strconv.Itoa(buf.Len()))
			return nil
		}

		// HTML
		if strings.Contains(contentType, "text/html") {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			doc, err := html.Parse(bytes.NewReader(bodyBytes))
			if err != nil {
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return nil
			}

			processSrcset := func(val string) string {
				parts := strings.Split(val, ",")
				var newParts []string
				for _, part := range parts {
					part = strings.TrimSpace(part)
					fields := strings.Fields(part)
					if len(fields) > 0 {
						fields[0] = wrapURL(fields[0])
						newParts = append(newParts, strings.Join(fields, " "))
					}
				}
				return strings.Join(newParts, ", ")
			}

			var f func(*html.Node)
			f = func(n *html.Node) {
				if n.Type == html.ElementNode {
					if n.Data == "meta" {
						for _, a := range n.Attr {
							if strings.EqualFold(a.Key, "http-equiv") && strings.EqualFold(a.Val, "Content-Security-Policy") {
								if n.Parent != nil {
									n.Parent.RemoveChild(n)
									return
								}
							}
						}
					}
					for i, a := range n.Attr {
						key := strings.ToLower(a.Key)
						val := a.Val
						switch key {
						case "src", "href", "data-src", "data-bg", "poster", "action", "xlink:href":
							n.Attr[i].Val = wrapURL(val)
						case "srcset", "data-srcset":
							n.Attr[i].Val = processSrcset(val)
						case "style":
							if strings.Contains(val, "url(") {
								reStyle := regexp.MustCompile(`url\(['"]?([^'"\)]+)['"]?\)`)
								n.Attr[i].Val = reStyle.ReplaceAllStringFunc(val, func(m string) string {
									sub := reStyle.FindStringSubmatch(m)
									if len(sub) < 2 {
										return m
									}
									return fmt.Sprintf("url('%s')", wrapURL(sub[1]))
								})
							}
						}
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					f(c)
				}
			}
			f(doc)

			// --- JS INJECTION: NAVIGATION API INTERCEPTOR ---
			targetBase := targetURL.Scheme + "://" + targetURL.Host
			targetBase = strings.TrimSuffix(targetBase, "/")

			scriptContent := `
			<script>
				(function() {
					const PROXY_BASE = '` + myProxyBase + `'; 
					const TARGET_ORIGIN = '` + targetBase + `';

					function wrap(u) {
						if(!u) return u;
						if(u.includes('/v1/proxy')) return u;
						if(u.startsWith('data:') || u.startsWith('javascript:') || u.startsWith('mailto:')) return u;

						let fullUrl = u;
						// Relatif URL (/checkout)
						if(u.startsWith('/')) {
							fullUrl = TARGET_ORIGIN + u;
						} 
						// Localhost sızıntısı (Browser çözümü)
						else if(u.startsWith(window.location.origin)) {
							fullUrl = TARGET_ORIGIN + u.replace(window.location.origin, '');
						}
						// Zaten hedef URL
						else if(u.startsWith(TARGET_ORIGIN)) {
							fullUrl = u;
						}
						// Başka http URL
						else if (u.startsWith('http')) {
							fullUrl = u;
						} 
						// Diğer
						else {
							fullUrl = TARGET_ORIGIN + '/' + u;
						}
						
						return PROXY_BASE + encodeURIComponent(fullUrl);
					}

					// 1. NAVIGATION API (Modern Browserlar için Kesin Çözüm)
					// Bu API, tıklamaları, formları, location.href değişikliklerini ve history API'yi TEK NOKTADAN yakalar.
					if (window.navigation) {
						navigation.addEventListener('navigate', (event) => {
							const url = new URL(event.destination.url);
							
							// Eğer URL bizim proxy formatımızda DEĞİLSE
							if (!url.href.includes('/v1/proxy')) {
								// Eğer localhost'a (kendi sitemize) gitmeye çalışıyorsa
								if (url.origin === window.location.origin) {
									event.preventDefault(); // Gitmeyi engelle
									
									// URL'i düzelt ve manuel git
									// url.pathname ve url.search kısmını alıp hedef siteyle birleştiriyoruz
									const path = url.pathname + url.search + url.hash;
									const newDest = wrap(path);
									
									// Sonsuz döngü koruması
									if(window.location.href !== newDest) {
										window.location.assign(newDest);
									}
								}
							}
						});
					}

					// 2. FALLBACK: History API Patch (Eski browserlar veya Navigation API'nin kaçırdığı durumlar)
					const originalPushState = history.pushState;
					const originalReplaceState = history.replaceState;

					history.pushState = function(state, unused, url) {
						if (url) {
							// URL'i sarmala ve git
							// Bu işlem React'ın "push" işlemini bizim proxy yönlendirmemize çevirir
							const newUrl = wrap(url);
							// Eğer Navigation API zaten yakalamadıysa
							if(window.location.href !== newUrl) {
								window.location.assign(newUrl); 
							}
							return; 
						}
						return originalPushState.apply(this, arguments);
					};

					history.replaceState = function(state, unused, url) {
						if (url) {
							// Sayfayı yenileme, sadece URL çubuğunu düzelt
							return originalReplaceState.call(this, state, unused, wrap(url));
						}
						return originalReplaceState.apply(this, arguments);
					};

					// 3. FALLBACK: Window Location Patch
					// location.assign ve location.replace fonksiyonlarını eziyoruz
					window.location.assign = function(url) {
						window.location.href = wrap(url);
					};
					window.location.replace = function(url) {
						window.location.href = wrap(url);
					};

					// 4. FALLBACK: Click Listener (A etiketleri için)
					document.addEventListener('click', function(e) {
						const link = e.target.closest('a, area');
						if (link) {
							const href = link.getAttribute('href'); 
							if (href && !href.startsWith('#') && !href.startsWith('javascript:') && !href.includes('/v1/proxy')) {
								e.preventDefault();
								e.stopImmediatePropagation();
								window.location.href = wrap(href);
							}
						}
					}, true);

				})();
			</script>`

			var buf bytes.Buffer
			html.Render(&buf, doc)
			finalHTML := buf.String()

			if strings.Contains(finalHTML, "<head>") {
				finalHTML = strings.Replace(finalHTML, "<head>", "<head>"+scriptContent, 1)
			} else {
				finalHTML = scriptContent + finalHTML
			}

			outBuf := bytes.NewBufferString(finalHTML)
			resp.Body = io.NopCloser(outBuf)
			resp.ContentLength = int64(outBuf.Len())
			resp.Header.Set("Content-Length", strconv.Itoa(outBuf.Len()))
			resp.Header.Set("Content-Type", "text/html; charset=utf-8")
			return nil
		}
		return nil
	}
	slog.Info("Proxying", slog.String("target", targetURL.String()))
	proxy.ServeHTTP(w, r)
}

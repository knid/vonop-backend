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
)

func (v *HttpViews) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	targetParam := r.URL.Query().Get("url")
	if targetParam == "" {
		http.Error(w, "url param is required", http.StatusBadRequest)
		return
	}

	targetURL, err := url.Parse(targetParam)
	if err != nil {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "wemob_target",
		Value:    targetURL.String(),
		Path:     "/",
		HttpOnly: true,
	})

	proxyURL, err := url.Parse(fmt.Sprintf("http://%s@%s", os.Getenv("PREVIEW_PROXY_AUTH"), os.Getenv("PREVIEW_PROXY_HOST")))
	if err != nil {
		slog.Error("Error parsing proxy URL: "+err.Error(),
			slog.String("from", "PROXY"),
			slog.String("user.id", user.ID.String()),
			slog.String("user.email", user.Email),
			slog.String("target.url", targetURL.String()),
			slog.String("proxy.host", os.Getenv("PREVIEW_PROXY_HOST")))
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
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
		req.Header.Del("If-None-Match")
		req.Header.Del("If-Modified-Since")
		req.Header.Del("X-Forwarded-For")
		req.Header.Del("X-Forwarded-Host")
		req.Header.Del("X-Forwarded-Proto")
		req.Header.Del("Forwarded")
		req.Header.Del("Via")

		req.Header.Set("Referer", targetURL.Scheme+"://"+targetURL.Host+"/")
		req.Header.Set("Origin", targetURL.Scheme+"://"+targetURL.Host)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode < 200 {
			resp.Header.Del("Link")
			return nil
		}

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location, err := resp.Location()
			if err == nil {
				redirectURL := location.String()

				if strings.HasPrefix(redirectURL, "/") {
					redirectURL = targetURL.Scheme + "://" + targetURL.Host + redirectURL
				}

				newLoc := "/v1/proxy?url=" + url.QueryEscape(redirectURL)
				resp.Header.Set("Location", newLoc)
			}
			return nil
		}

		cookies := resp.Cookies()
		resp.Header.Del("Set-Cookie")

		for _, cookie := range cookies {
			cookie.Domain = ""
			cookie.Secure = false
			cookie.SameSite = http.SameSiteLaxMode

			resp.Header.Add("Set-Cookie", cookie.String())
		}

		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Disposition")
		resp.Header.Del("Transfer-Encoding")
		resp.Header.Del("Content-Encoding")

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location, err := resp.Location()
			if err == nil {
				newLoc := "/v1/proxy?url=" + url.QueryEscape(location.String())
				resp.Header.Set("Location", newLoc)
			}
			return nil
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}
		resp.Body.Close()

		htmlContent := string(bodyBytes)

		reCSP := regexp.MustCompile(`(?i)<meta\s+http-equiv=["']Content-Security-Policy["'][^>]*>`)
		htmlContent = reCSP.ReplaceAllString(htmlContent, "")

		reIntegrity := regexp.MustCompile(`\sintegrity=["'][^"']*["']`)
		htmlContent = reIntegrity.ReplaceAllString(htmlContent, "")

		reXlink := regexp.MustCompile(`xlink:href=["']([^"']+)["']`)
		htmlContent = reXlink.ReplaceAllStringFunc(htmlContent, func(match string) string {
			// match: xlink:href="/styles/icons.svg#search"
			content := match[12 : len(match)-1]

			if strings.HasPrefix(content, "data:") || strings.HasPrefix(content, "#") {
				return match
			}

			fullUrl := content
			if strings.HasPrefix(content, "//") {
				fullUrl = targetURL.Scheme + ":" + content
			} else if strings.HasPrefix(content, "/") {
				fullUrl = targetURL.Scheme + "://" + targetURL.Host + content
			} else if !strings.HasPrefix(content, "http") {
				fullUrl = targetURL.String() + content
			}

			proxyUrl := "/v1/proxy?url=" + url.QueryEscape(fullUrl)
			return `xlink:href="` + proxyUrl + `"`
		})

		targetURLStr := targetURL.Scheme + "://" + targetURL.Host + "/"
		baseTag := `<base href="` + targetURLStr + `">`

		scriptTag := `
        <script>
        function wrapUrl(url) {
            if (!url) return url;
            if (url.startsWith('data:') || url.startsWith('javascript:') || url.startsWith('mailto:')) return url;
            if (url.startsWith('#')) return url;

            var proxyOrigin = window.location.origin + '/v1/proxy';
            if (url.startsWith(proxyOrigin)) return url;

            var urlPart = url;
            var hashPart = "";
            var hashIdx = url.indexOf('#');
            if (hashIdx > -1) {
                urlPart = url.substring(0, hashIdx);
                hashPart = url.substring(hashIdx);
            }

            var fullUrl = urlPart;
            if (fullUrl.startsWith('http')) {
            } else {
                var base = document.querySelector('base').href;
                if (base.endsWith('/') && fullUrl.startsWith('/')) fullUrl = fullUrl.substring(1);
                fullUrl = base + fullUrl;
            }

            return window.location.origin + '/v1/proxy?url=' + encodeURIComponent(fullUrl) + hashPart;
        }

        function fixInlineJs() {
            var elements = document.querySelectorAll('[onclick], [onmousedown], [onmouseup]');
            
            elements.forEach(function(el) {
                if (el.getAttribute('data-proxy-patched')) return;

                ['onclick', 'onmousedown', 'onmouseup'].forEach(function(attr) {
                    var code = el.getAttribute(attr);
                    if (!code) return;

                    var newCode = code.replace(
                        /(window\.location|location\.href|location)\s*=\s*['"]([^'"]+)['"]/gi, 
                        function(match, variable, url) {
                            if (url.includes('/v1/proxy')) return match;
                            return variable + "=wrapUrl('" + url + "')";
                        }
                    );

                    if (code !== newCode) {
                        el.setAttribute(attr, newCode);
                        el.setAttribute('data-proxy-patched', 'true'); // İşaretle
                    }
                });
            });
        }

        window.addEventListener('load', function() {
            fixInlineJs();    
            fixSvgs();         
            fixSrcset();      
        });
        
        setInterval(fixInlineJs, 1000); 
        setInterval(fixSvgs, 2000);

        function fixSvgs() {
            document.querySelectorAll('use').forEach(el => {
                let xlink = el.getAttribute('xlink:href');
                if (xlink && !xlink.startsWith('data:') && !xlink.includes('/v1/proxy')) {
                    el.setAttribute('xlink:href', wrapUrl(xlink));
                }
                let href = el.getAttribute('href');
                if (href && !href.startsWith('data:') && !href.includes('/v1/proxy')) {
                    el.setAttribute('href', wrapUrl(href));
                }
            });
        }

        function fixSrcset() {
            document.querySelectorAll('[srcset]').forEach(el => {
                let srcset = el.getAttribute('srcset');
                if (!srcset || srcset.includes('/v1/proxy')) return;
                let newSrcset = srcset.split(',').map(part => {
                    part = part.trim();
                    let spaceIdx = part.indexOf(' ');
                    let url = spaceIdx > -1 ? part.substring(0, spaceIdx) : part;
                    let desc = spaceIdx > -1 ? part.substring(spaceIdx) : '';
                    return wrapUrl(url) + desc;
                }).join(', ');
                el.setAttribute('srcset', newSrcset);
            });
        }

        const originalLocation = window.location;
        window.location.assign = function(url) { window.location.href = wrapUrl(url); };
        window.location.replace = function(url) { window.location.href = wrapUrl(url); };

        const originalFetch = window.fetch;
        window.fetch = function(input, init) {
            if (typeof input === 'string') input = wrapUrl(input);
            else if (input instanceof Request) input = wrapUrl(input.url);
            return originalFetch(input, init);
        };

        const originalOpen = XMLHttpRequest.prototype.open;
        XMLHttpRequest.prototype.open = function(method, url, async, user, password) {
            return originalOpen.apply(this, [method, wrapUrl(url), async, user, password]);
        };

        document.addEventListener('click', function(e) {
            var target = e.target.closest('a');
            if (!target) return;
            var href = target.getAttribute('href');
            if (!href || href.startsWith('#') || href.startsWith('javascript:') || href.startsWith('mailto:')) return;
            e.preventDefault();
            window.location.href = wrapUrl(target.href);
        });

        document.addEventListener('submit', function(e) {
            var form = e.target;
            var action = form.action;
            if (action) {
                var newAction = wrapUrl(action);
                if (form.action !== newAction) form.action = newAction;
            }
        });
        </script>
        `

		if strings.Contains(htmlContent, "<head>") {
			injectContent := "<head>" + baseTag + scriptTag
			htmlContent = strings.Replace(htmlContent, "<head>", injectContent, 1)
		} else {
			htmlContent = baseTag + scriptTag + htmlContent
		}

		buf := bytes.NewBufferString(htmlContent)
		resp.Body = io.NopCloser(buf)

		resp.ContentLength = int64(buf.Len())
		resp.Header.Set("Content-Length", strconv.Itoa(buf.Len()))
		resp.Header.Set("Content-Type", "text/html; charset=utf-8")

		return nil
	}

	slog.Info("Proxying request to "+targetURL.String(),
		slog.String("from", "PROXY"),
		slog.String("user.id", user.ID.String()),
		slog.String("user.email", user.Email),
		slog.String("target.url", targetURL.String()),
		slog.String("proxy.host", os.Getenv("PREVIEW_PROXY_HOST")),
	)

	proxy.ServeHTTP(w, r)
}

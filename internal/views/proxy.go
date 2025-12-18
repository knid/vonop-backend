package views

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func (v *HttpViews) ProxyHandler(w http.ResponseWriter, r *http.Request) {

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

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

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
		req.Header.Set("Origin", targetURL.Scheme+"://"+targetURL.Host)
		req.Header.Set("Referer", targetURL.String())

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		// req.Header.Del("Cookie")
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

		targetURLStr := targetURL.Scheme + "://" + targetURL.Host + "/"
		baseTag := `<base href="` + targetURLStr + `">`

		scriptTag := `
        <script>
        function wrapUrl(url) {
            if (!url) return url;
            if (url.startsWith('data:') || url.startsWith('#') || url.startsWith('javascript:') || url.startsWith('mailto:')) return url;
            
            var proxyOrigin = window.location.origin + '/v1/proxy';
            if (url.startsWith(proxyOrigin)) return url;

            if (url.startsWith('http')) {
                return window.location.origin + '/v1/proxy?url=' + encodeURIComponent(url);
            }
            
            var base = document.querySelector('base').href;
            if (base.endsWith('/') && url.startsWith('/')) url = url.substring(1);
            var fullUrl = base + url;
            
            return window.location.origin + '/v1/proxy?url=' + encodeURIComponent(fullUrl);
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

	proxy.ServeHTTP(w, r)
}

func handleFallback(w http.ResponseWriter, r *http.Request) {
	ignoredPaths := []string{"/.well-known", "/monorail", "/produce_batch", "/shop_events", "/wpm@"}
	for _, path := range ignoredPaths {
		if strings.Contains(r.URL.Path, path) {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	cookie, err := r.Cookie("wemob_target")
	if err != nil || cookie.Value == "" {
		http.NotFound(w, r)
		return
	}

	targetURL, err := url.Parse(cookie.Value)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	proxy.Director = func(req *http.Request) {
		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		req.Header.Del("Accept-Encoding")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		return nil
	}

	proxy.ServeHTTP(w, r)
}

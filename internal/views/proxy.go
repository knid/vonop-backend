package views

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

func (v *HttpViews) ProxyHandler(w http.ResponseWriter, r *http.Request) {

	targetParam := r.URL.Query().Get("url")
	if targetParam == "" {
		http.Error(w, "Url parametresi gerekli", http.StatusBadRequest)
		return
	}

	targetURL, err := url.Parse(targetParam)
	if err != nil {
		http.Error(w, "Geçersiz URL", http.StatusBadRequest)
		return
	}

	// Cookie'ye hedefi kaydet
	http.SetCookie(w, &http.Cookie{
		Name:     "wemob_target",
		Value:    targetURL.String(),
		Path:     "/",
		HttpOnly: true,
	})

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// --- DIRECTOR (İSTEK DÜZENLEYİCİ) ---
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.URL.Path = targetURL.Path
		req.URL.RawQuery = targetURL.RawQuery
		req.RequestURI = ""

		// KRİTİK DEĞİŞİKLİK 1: SIKIŞTIRMAYI İPTAL ET
		// Sunucuya "Ben sıkıştırma desteklemiyorum, bana DÜZ METİN (Plain Text) yolla" diyoruz.
		// Bu sayede Gzip/Brotli çözme hatası veya bozuk dosya inme riski SIFIRA iniyor.
		req.Header.Del("Accept-Encoding")

		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		// req.Header.Del("Cookie")
	}

	// --- MODIFY RESPONSE (YANIT DÜZENLEYİCİ) ---
	proxy.ModifyResponse = func(resp *http.Response) error {
		// 1. HTTP 103 (Early Hints) gibi ara yanıtları yoksay
		if resp.StatusCode < 200 {
			resp.Header.Del("Link") // Tarayıcının önden yükleme yapmasını engelle
			return nil
		}

		cookies := resp.Cookies()
		resp.Header.Del("Set-Cookie") // Orijinal, domain kısıtlamalı çerezleri sil

		for _, cookie := range cookies {
			cookie.Domain = ""                     // Domain'i boşalt (Localhost kabul etsin)
			cookie.Secure = false                  // Localhost genelde http olduğu için secure kaldırılabilir
			cookie.SameSite = http.SameSiteLaxMode // İframe/Proxy uyumu için

			// Düzenlenmiş çerezi tekrar ekle
			resp.Header.Add("Set-Cookie", cookie.String())
		}

		// 2. İndirmeyi ve Sorun Çıkaran Headerları SİL
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Disposition") // İndirme emrini sil
		resp.Header.Del("Transfer-Encoding")   // Chunked veriyi iptal et
		resp.Header.Del("Content-Encoding")    // Sıkıştırma başlığını sil (Zaten düz metin istedik)

		// 3. Redirect Yakalama
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location, err := resp.Location()
			if err == nil {
				newLoc := "/v1/proxy?url=" + url.QueryEscape(location.String())
				resp.Header.Set("Location", newLoc)
			}
			return nil
		}

		// 4. Sadece HTML ise işle
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		// 5. Body Okuma (ARTIK GZIP YOK, DİREKT OKU)
		// Request'te sıkıştırmayı kapattığımız için burası her zaman düz metindir.
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}
		resp.Body.Close()

		// 6. HTML Manipülasyonu
		htmlContent := string(bodyBytes)

		targetURLStr := targetURL.Scheme + "://" + targetURL.Host + "/"
		baseTag := `<base href="` + targetURLStr + `">`

		scriptTag := `
        <script>
        // 1. LİNKLERİ YAKALA (Mevcut kodun)
        document.addEventListener('click', function(e) {
            var target = e.target.closest('a');
            if (!target) return;

            var href = target.getAttribute('href');
            if (!href || href.startsWith('#') || href.startsWith('javascript:') || 
                href.startsWith('mailto:') || href.startsWith('tel:') || href.startsWith('sms:') || href.startsWith('whatsapp:')) {
                return;
            }

            e.preventDefault();
            var fullUrl = target.href;
            
            // Recursive URL temizliği
            try {
                var urlObj = new URL(fullUrl);
                if(urlObj.searchParams.has("url") && urlObj.searchParams.get("url").startsWith("http")) {
                     fullUrl = urlObj.searchParams.get("url");
                }
            } catch(ex) {}

            var proxyHost = window.location.origin;
            window.location.href = proxyHost + '/v1/proxy?url=' + encodeURIComponent(fullUrl);
        });

        // 2. FORMLARI YAKALA (YENİ EKLENEN KISIM)
        // Sepete ekle butonları bir form submit eder. Bunu yakalamazsak kaçar.
        document.addEventListener('submit', function(e) {
            var form = e.target;
            var action = form.action; // Base tag sayesinde full url gelir (https://shootgrip.com/cart/add)

            // Eğer zaten proxy üzerindeyse dokunma
            var proxyHost = window.location.origin;
            if (action.startsWith(proxyHost)) return;

            // Formun action adresini bizim proxy adresimize çeviriyoruz
            // Tarayıcı POST verilerini alıp bu yeni adrese gönderecek.
            // Backend de bu verileri alıp gerçek adrese iletecek.
            var newAction = proxyHost + '/v1/proxy?url=' + encodeURIComponent(action);
            
            form.action = newAction;
            // preventDefault yapmıyoruz, formun yeni adrese normal şekilde gitmesine izin veriyoruz.
        });
        </script>
        `

		if strings.Contains(htmlContent, "<head>") {
			injectContent := "<head>" + baseTag + scriptTag
			htmlContent = strings.Replace(htmlContent, "<head>", injectContent, 1)
		} else {
			htmlContent = baseTag + scriptTag + htmlContent
		}

		// 7. Yeni Body'yi Oluştur
		buf := bytes.NewBufferString(htmlContent)
		resp.Body = io.NopCloser(buf)

		// 8. KRİTİK: Tarayıcıya bunun HTML olduğunu ZORLA
		resp.ContentLength = int64(buf.Len())
		resp.Header.Set("Content-Length", strconv.Itoa(buf.Len()))
		// Charset ekleyerek tarayıcının doğru render etmesini garantile
		resp.Header.Set("Content-Type", "text/html; charset=utf-8")

		return nil
	}

	proxy.ServeHTTP(w, r)
}

func handleFallback(w http.ResponseWriter, r *http.Request) {
	// Gereksiz logları sustur
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

		// Fallback isteklerinde de sıkıştırmayı kapatalım ki sorun çıkmasın
		req.Header.Del("Accept-Encoding")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		return nil
	}

	proxy.ServeHTTP(w, r)
}

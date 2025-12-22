package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/knid/wemob/internal/constants"
)

func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	UpstreamProxyHost := os.Getenv("PREVIEW_PROXY_HOST")
	UpstreamProxyAuth := os.Getenv("PREVIEW_PROXY_AUTH")

	// 1. Yeni Oturum Başlatma (?url=...)
	// Kullanıcı siteye ilk kez giriş yapıyorsa veya site değiştirmek istiyorsa.
	queryParam := r.URL.Query().Get("url")
	if queryParam != "" {
		targetURL, err := url.Parse(queryParam)
		if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
			http.Error(w, "Invalid 'url' parameter. Must be absolute (e.g., https://site.com)", http.StatusBadRequest)
			return
		}

		// Cookie oluştur ve tarayıcıya ver
		http.SetCookie(w, &http.Cookie{
			Name:     constants.SessionCookieName,
			Value:    targetURL.String(),
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteNoneMode,
			Expires:  time.Now().Add(24 * time.Hour),
		})

		slog.Info("🌐 New proxy session started", "target", targetURL.String(), "from", "PROXY")

		// Temiz bir başlangıç için ana sayfaya yönlendir
		// Tarayıcı artık cookiye sahip olduğu için bir sonraki istekte proxy devreye girecek.
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// 2. Mevcut Oturumu Kontrol Et
	cookie, err := r.Cookie(constants.SessionCookieName)
	if err != nil || cookie.Value == "" {
		// Session yoksa ve favicon gibi gereksiz bir şeyse 404 ver
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Ana sayfaya session olmadan geldiyse kullanım mesajı göster
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `<h1>No Active Session</h1><p>Please use <code>?url=https://target.com</code> to start.</p>`)
		return
	}

	targetBase := cookie.Value
	targetURL, err := url.Parse(targetBase)
	if err != nil {
		http.Error(w, "Invalid session target", http.StatusInternalServerError)
		return
	}

	slog.Info("🔁 Existing proxy session used", "target", targetURL.String(), "from", "PROXY")

	// 3. Reverse Proxy Oluştur
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Upstream Proxy Ayarları (Varsa)
	if UpstreamProxyHost != "" {
		upstreamUrl := &url.URL{
			Scheme: "http",
			Host:   UpstreamProxyHost,
		}
		if UpstreamProxyAuth != "" {
			parts := strings.Split(UpstreamProxyAuth, ":")
			if len(parts) == 2 {
				upstreamUrl.User = url.UserPassword(parts[0], parts[1])
			}
		}

		proxy.Transport = &http.Transport{
			Proxy: http.ProxyURL(upstreamUrl),
		}
	}

	// 4. Request Düzenleme (Director)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Host başlığını hedef siteye göre ayarla (Çok önemli, yoksa 404 alırsın)
		req.Host = targetURL.Host
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host

		// Path'i olduğu gibi koru (Catch-All mantığı)
		// /checkout geldiyse hedef siteye de /checkout gitmeli.

		// Gereksiz Headerları Temizle
		req.Header.Del("Accept-Encoding") // Response'u manipüle edebilmek için gzip kapalı olmalı (veya decode etmeliyiz)
		req.Header.Del("X-Forwarded-For")

		// Tarayıcı gibi davran
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", targetURL.String())
		req.Header.Set("Origin", targetURL.Scheme+"://"+targetURL.Host)
	}

	// 5. Response Düzenleme (ModifyResponse)
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Güvenlik Duvarlarını Kaldır (Iframe ve CORS için)
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")
		resp.Header.Del("Strict-Transport-Security")
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Credentials", "true")

		// 301/302 Redirect Düzeltme
		// Hedef site bizi https://google.com'a atıyorsa karışma.
		// Ama https://gumastore.com/cart'a atıyorsa, bunu /cart yapmalıyız ki bizde kalsın.
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc != "" {
				u, err := url.Parse(loc)
				if err == nil {
					// Eğer redirect hedef domain içindeyse, host kısmını sil, sadece path kalsın.
					if u.Host == targetURL.Host || u.Host == "" {
						newPath := u.Path
						if u.RawQuery != "" {
							newPath += "?" + u.RawQuery
						}
						resp.Header.Set("Location", newPath)
					}
				}
			}
		}

		// Sadece HTML içeriği düzenle
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(contentType, "text/html") {
			return rewriteHTML(resp, targetURL)
		}

		return nil
	}

	// Hata Yönetimi
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy Error: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, "Proxy Error: %v", err)
	}

	proxy.ServeHTTP(w, r)
}

// rewriteHTML: HTML içindeki absolute linkleri (https://gumastore.com/...)
// relative linklere (/...) çevirir. Bu sayede kullanıcı proxy'de kalır.
func rewriteHTML(resp *http.Response, targetURL *url.URL) error {
	// Body'yi oku
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Eğer Gzip ise aç (Bazı sunucular Accept-Encoding headerını dinlemez)
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(bytes.NewReader(bodyBytes))
		if err == nil {
			bodyBytes, _ = io.ReadAll(gr)
			gr.Close()
			resp.Header.Del("Content-Encoding") // Artık düz text
		}
	}

	htmlContent := string(bodyBytes)

	// 1. Base Tag'i Sil
	// Base tag relative linkleri bozar, proxy ortamında en büyük düşmandır.
	reBase := regexp.MustCompile(`(?i)<base[^>]*>`)
	htmlContent = reBase.ReplaceAllString(htmlContent, "")

	// 2. Domain Stripping (Domain Temizliği) - EN KRİTİK KISIM
	// Sayfadaki "https://gumastore.com/checkout" linklerini "/checkout" yapar.
	// Böylece linke tıklandığında tarayıcı "proxy.wemob.io/checkout" adresine gider.
	// Catch-All router'ımız da bunu yakalayıp tekrar hedefe iletir.

	targetString := targetURL.String() // https://gumastore.com
	targetStringNoSlash := strings.TrimRight(targetString, "/")

	// Hem https://... hem http://... versiyonlarını temizle
	htmlContent = strings.ReplaceAll(htmlContent, targetString, "")
	htmlContent = strings.ReplaceAll(htmlContent, targetStringNoSlash, "")

	// Çıplak domainleri de temizlemeyi deneyebiliriz ama riskli olabilir.
	// Şimdilik sadece tam protokol içerenleri temizliyoruz.

	// 3. Integrity Attribute Silme (SRI)
	// İçeriği değiştirdiğimiz için (base tag silme vb.) scriptlerin hash'i tutmayacaktır.
	// Browser engellemesin diye integrity kontrolünü siliyoruz.
	reIntegrity := regexp.MustCompile(`\s*integrity=["'][^"']*["']`)
	htmlContent = reIntegrity.ReplaceAllString(htmlContent, "")

	// Yeni body'yi yaz
	buf := bytes.NewBufferString(htmlContent)
	resp.Body = io.NopCloser(buf)
	resp.ContentLength = int64(buf.Len())
	resp.Header.Set("Content-Length", strconv.Itoa(buf.Len()))

	return nil
}

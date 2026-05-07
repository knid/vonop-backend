package utils

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
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
}

// fetchHTML downloads the HTML of the given page via GET.
func FetchHTML(targetURL, proxyHost, proxyAuth string) (string, error) {

	if proxyHost != "" {
		upstreamUrl := &url.URL{Scheme: "http", Host: proxyHost}
		if proxyAuth != "" {
			parts := strings.Split(proxyAuth, ":")
			if len(parts) == 2 {
				upstreamUrl.User = url.UserPassword(parts[0], parts[1])
			}
		}
		customTransport.Proxy = http.ProxyURL(upstreamUrl)
	}

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: customTransport,
	}

	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")

	resp, err := client.Do(req)

	if err != nil {
		return "", fmt.Errorf("failed to GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %v", err)
	}
	return string(body), nil
}

// SiteMetadata holds basic information extracted from a web page's HTML.
type SiteMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Favicon     string `json:"favicon"`
}


// extractAllURLs parses HTML and collects all unique normalized hrefs.
func ExtractAllURLs(htmlContent string, baseURL string) []string {
	tokenizer := html.NewTokenizer(strings.NewReader(htmlContent))
	base, _ := url.Parse(baseURL)
	seen := make(map[string]bool)
	var results []string

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		if token.DataAtom.String() != "a" {
			continue
		}
		for _, attr := range token.Attr {
			if attr.Key != "href" {
				continue
			}
			href := strings.TrimSpace(attr.Val)
			if href == "" ||
				strings.HasPrefix(href, "#") ||
				strings.HasPrefix(strings.ToLower(href), "javascript") {
				continue
			}

			parsed, err := url.Parse(href)
			if err != nil {
				continue
			}
			full := base.ResolveReference(parsed)
			finalURL := full.String()

			// optional: skip mailto:, tel:, etc.
			if strings.HasPrefix(finalURL, "mailto:") || strings.HasPrefix(finalURL, "tel:") {
				continue
			}

			if !seen[finalURL] {
				seen[finalURL] = true
				results = append(results, finalURL)
			}
		}
	}

	sort.Strings(results)
	return results
}


// ExtractJSON finds the outermost JSON object or array in a string by matching braces/brackets.
func ExtractJSON(raw string) string {
	startObj := strings.Index(raw, "{")
	startArr := strings.Index(raw, "[")

	start := -1
	openChar := byte('{')
	closeChar := byte('}')

	if startObj >= 0 && (startArr < 0 || startObj <= startArr) {
		start = startObj
		openChar = '{'
		closeChar = '}'
	} else if startArr >= 0 {
		start = startArr
		openChar = '['
		closeChar = ']'
	}

	if start < 0 {
		return raw
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == openChar {
			depth++
		} else if ch == closeChar {
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}

	// No balanced closing found — return from start to end as best effort
	return raw[start:]
}

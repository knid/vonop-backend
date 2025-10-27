package utils

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// fetchHTML downloads the HTML of the given page via GET.
func FetchHTML(targetURL string) (string, error) {
	resp, err := http.Get(targetURL)
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

func CleanLLMOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

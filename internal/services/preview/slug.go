package preview

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

const defaultPreviewDomain = "preview.wemob.app"

type SlugOptions struct {
	ProjectSlug string
	ProjectID   string
	JobID       string
	Domain      string
}

func NewSlug(opts SlugOptions) (string, error) {
	prefix := normalizePrefix(opts.ProjectSlug)
	if prefix == "" {
		prefix = "p-" + shortID(opts.ProjectID)
	}
	if prefix == "p-" {
		prefix = "p"
	}
	suffix, err := randomSuffix(5)
	if err != nil {
		return "", err
	}
	if job := shortID(opts.JobID); job != "" {
		return trimSlug(fmt.Sprintf("%s-r-%s-%s", prefix, job, suffix)), nil
	}
	return trimSlug(fmt.Sprintf("%s-%s", prefix, suffix)), nil
}

func PublicURL(slug string, domain string) string {
	if strings.TrimSpace(domain) == "" {
		domain = defaultPreviewDomain
	}
	return "https://" + slug + "." + strings.TrimPrefix(domain, ".")
}

func normalizePrefix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return trimSlug(strings.Trim(b.String(), "-"))
}

func trimSlug(slug string) string {
	slug = strings.Trim(slug, "-")
	if len(slug) <= 48 {
		return slug
	}
	return strings.Trim(slug[:48], "-")
}

func shortID(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(value) <= 6 {
		return value
	}
	return value[:6]
}

func randomSuffix(length int) (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("preview: random suffix: %w", err)
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	if len(encoded) < length {
		return encoded, nil
	}
	return encoded[:length], nil
}

package controllers

import (
	"context"
	"crypto/rand"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

const slugSuffixCharset = "abcdefghijklmnopqrstuvwxyz0123456789"
const slugSuffixLen = 5
const slugMaxBaseLen = 50

// generateAppNameAndSlug asks a fast LLM to produce a short English display
// name for the project from the user's (possibly non-English) prompt, then
// derives a URL-safe slug with a 5-char random suffix. Falls back to a
// truncated prompt if the LLM call fails.
//
// The optional `user` is purely for PostHog attribution. Pass nil to skip
// the trace.
func (c *Controller) generateAppNameAndSlug(ctx context.Context, prompt string, user *models.User) (string, string) {
	provider := c.llmProviderForUser(user)
	model := provider.FastModel()

	tracker := utils.NewTokenTracker()
	tracker.TrackContent = true // ensure the exact name-generation prompt + model output are captured in PostHog LLM traces
	if user != nil {
		tracker.UserID = user.ID.String()
		if orgID, orgErr := c.RequireOrgIDForUser(user, nil); orgErr == nil {
			tracker.OrganizationID = orgID.String()
		}
	}
	ctx = utils.WithTokenTracker(ctx, tracker)

	system := `You name apps. Given a project description in any language, respond with a short 2-4 word English app name in Title Case. No punctuation, emoji, quotes, or explanation — just the name.

Examples:
- "çok basit ai chat uygulaması" → Simple AI Chat
- "telefonla yemeklerin resimlerini çekip yapay zeka ile tarif çıkaran uygulama" → AI Recipe Scanner
- "online clothing store for men" → Mens Clothing Store
- "dashboard for tracking my team's OKRs" → Team OKR Tracker`

	result, err := utils.GetTextFromLLM(ctx, model, system, prompt, provider, 64)
	if user != nil {
		c.RecordTokenUsage(user, "project_name", model, provider.Name, "", "standard", tracker)
	}
	if err != nil {
		slog.Warn("LLM name generation failed, falling back to truncated prompt", "error", err)
		if user != nil {
			c.RecordLLMError(user, tracker, "project_name", model, provider.Name, "", "standard", 0, "name_gen_failed", err.Error())
		}
		return fallbackNameAndSlug(prompt)
	}

	name := sanitizeAppName(result)
	if name == "" {
		return fallbackNameAndSlug(prompt)
	}
	return name, makeSlug(name)
}

// makeSlug slugifies base, caps it, and appends a random alphanumeric suffix.
func makeSlug(base string) string {
	slug := utils.Slugify(base)
	if len(slug) > slugMaxBaseLen {
		slug = strings.Trim(slug[:slugMaxBaseLen], "-")
	}
	if slug == "" {
		slug = "app"
	}
	return slug + "-" + randomAlnum(slugSuffixLen)
}

// sanitizeAppName trims whitespace, quote wrappers, trailing punctuation, and
// caps length. LLMs sometimes return a newline-separated explanation even when
// told not to; keep only the first line.
func sanitizeAppName(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".,!?")
	if len(s) > slugMaxBaseLen {
		s = s[:slugMaxBaseLen]
	}
	return s
}

func fallbackNameAndSlug(prompt string) (string, string) {
	name := strings.TrimSpace(prompt)
	if len(name) > slugMaxBaseLen {
		name = name[:slugMaxBaseLen]
	}
	if name == "" {
		name = "App"
	}
	return name, makeSlug(name)
}

func randomAlnum(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = slugSuffixCharset[i%len(slugSuffixCharset)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = slugSuffixCharset[int(b[i])%len(slugSuffixCharset)]
	}
	return string(b)
}

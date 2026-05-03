package controllers

import (
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
)

// scaffoldMobileLandingPage writes a baseline marketing landing page into the
// project's repo at backend/app/page.tsx + backend/app/layout.tsx +
// backend/app/globals.css. The Fly-deployed Next.js backend then serves this
// as the root route at {slug}.wemob.app/.
//
// The scaffold is deterministic Go code (zero AI credits). The agent iterates
// on these files via the normal chat-turn flow afterwards. We seed a clear
// baseline so:
//  1. The user has SOMETHING to look at on day 1, even before chatting with
//     the AI.
//  2. The AI has a known starting structure to edit instead of a blank canvas.
//  3. Store-badge placeholders are obvious — the agent swaps them for real
//     <a> wrappers once read_mobile_config reports app_store_url +
//     play_store_url.
//
// All three files include a comment at the top noting the AI agent owns
// them and may rewrite freely. Errors here are NEVER fatal — the caller logs
// + continues so a flaky disk doesn't fail project creation.
func scaffoldMobileLandingPage(repo *gitrepo.Repo, project *models.AppProject) error {
	if repo == nil {
		return fmt.Errorf("scaffoldMobileLandingPage: nil repo")
	}
	if project == nil {
		return fmt.Errorf("scaffoldMobileLandingPage: nil project")
	}

	files := map[string]string{
		"backend/app/page.tsx":    landingPageTSX(project),
		"backend/app/layout.tsx":  landingLayoutTSX(project),
		"backend/app/globals.css": landingGlobalsCSS,
	}

	for path, content := range files {
		if err := repo.WriteFile(path, []byte(content)); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// landingPageTSX returns the default app/page.tsx — a server-rendered
// marketing landing page with project name, prompt-derived tagline, and
// "Coming soon" placeholders for App Store + Play Store CTAs.
func landingPageTSX(project *models.AppProject) string {
	tagline := project.Prompt
	if len(tagline) > 200 {
		tagline = tagline[:200] + "…"
	}
	// Escape backticks + ${ — we're embedding into a TS template literal.
	// JSX text is fine for everything else; quotes are auto-encoded.
	tagline = escapeJSXText(tagline)
	name := escapeJSXText(project.Name)

	return `// Marketing landing page served at {slug}.wemob.app/.
// The AI agent owns this file — feel free to rewrite it. The mobile app's
// API routes live in backend/app/api/*/route.ts and are unrelated.
//
// Once the user publishes to the stores, the agent swaps the "Coming soon"
// CTAs below for real <a href={appStoreURL}>...</a> + <a href={playStoreURL}>...</a>
// wrappers. Read store URLs via read_mobile_config.

export default function LandingPage() {
  return (
    <main
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: "48px 24px",
        background: "linear-gradient(180deg, #f8fafc 0%, #eef2ff 100%)",
        fontFamily: "system-ui, -apple-system, sans-serif",
        color: "#0f172a",
        textAlign: "center",
      }}
    >
      <div style={{ maxWidth: 720 }}>
        <h1
          style={{
            fontSize: "clamp(2.5rem, 6vw, 4rem)",
            fontWeight: 700,
            margin: "0 0 16px",
            letterSpacing: "-0.02em",
          }}
        >
          ` + name + `
        </h1>
        <p
          style={{
            fontSize: "clamp(1.1rem, 2.4vw, 1.4rem)",
            color: "#475569",
            margin: "0 0 48px",
            lineHeight: 1.5,
          }}
        >
          ` + tagline + `
        </p>

        {/* App Store + Play Store CTAs.
            Coming-soon today; the agent swaps these for real <a> tags once
            read_mobile_config reports app_store_url + play_store_url. */}
        <div
          style={{
            display: "flex",
            gap: 16,
            justifyContent: "center",
            flexWrap: "wrap",
          }}
        >
          <span
            style={{
              padding: "14px 28px",
              borderRadius: 12,
              background: "#0f172a",
              color: "white",
              fontSize: "1rem",
              fontWeight: 500,
              opacity: 0.5,
              cursor: "not-allowed",
            }}
          >
            App Store — Coming soon
          </span>
          <span
            style={{
              padding: "14px 28px",
              borderRadius: 12,
              background: "#0f172a",
              color: "white",
              fontSize: "1rem",
              fontWeight: 500,
              opacity: 0.5,
              cursor: "not-allowed",
            }}
          >
            Play Store — Coming soon
          </span>
        </div>

        <footer
          style={{
            marginTop: 96,
            fontSize: "0.875rem",
            color: "#94a3b8",
          }}
        >
          Built with Wemob
        </footer>
      </div>
    </main>
  );
}
`
}

// landingLayoutTSX returns the default app/layout.tsx — a root layout with
// Next.js metadata API populated for OG / Twitter cards and a global CSS
// import. The agent overrides the metadata + adds <html>-level wrappers as
// the user iterates.
func landingLayoutTSX(project *models.AppProject) string {
	tagline := project.Prompt
	if len(tagline) > 160 {
		tagline = tagline[:160] + "…"
	}
	// In TS string-literal context: escape " and \ for safety.
	name := escapeTSStringLiteral(project.Name)
	desc := escapeTSStringLiteral(tagline)

	return `// Root layout for the marketing landing at {slug}.wemob.app/.
// The AI agent owns this file — feel free to rewrite metadata + wrappers as
// the project evolves. Don't put <head> tags directly in page.tsx — Next.js
// owns <head> from this layout's metadata export.

import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "` + name + `",
  description: "` + desc + `",
  openGraph: {
    title: "` + name + `",
    description: "` + desc + `",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: "` + name + `",
    description: "` + desc + `",
  },
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
`
}

// landingGlobalsCSS is the minimal CSS reset we ship so the scaffolded
// landing renders consistently across browsers. The agent can swap this for
// Tailwind / styled-components / whatever the user asks for.
const landingGlobalsCSS = `/* Minimal CSS reset for the marketing landing.
   The AI agent owns this file — rewrite as needed. */

*, *::before, *::after {
  box-sizing: border-box;
}

html, body {
  margin: 0;
  padding: 0;
  font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
    Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

body {
  background: #ffffff;
  color: #0f172a;
}
`

// escapeJSXText escapes the small set of characters that can break JSX text.
// In JSX, raw `{` / `}` start expressions and raw `<` / `>` start tags. The
// other characters (quotes, backslashes, ampersands) are encoded by React
// at render time. We keep this minimal — the prompt is user input and was
// already validated upstream; we just guard against accidental syntax breaks.
func escapeJSXText(s string) string {
	r := strings.NewReplacer(
		"{", "&#123;",
		"}", "&#125;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// escapeTSStringLiteral escapes `"` and `\` so we can embed a value inside a
// double-quoted TypeScript string literal in generated code.
func escapeTSStringLiteral(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
	)
	return r.Replace(s)
}

package templates

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// NonVariableFonts maps Google Fonts that are NOT variable-width and require
// an explicit weight parameter in next/font/google. The value is the list of
// available weights. If a font is not in this map, we assume it's a variable
// font and omit the weight parameter (Next.js handles it automatically).
var NonVariableFonts = map[string][]string{
	"Bebas Neue":         {"400"},
	"Racing Sans One":    {"400"},
	"Bungee":             {"400"},
	"Bungee Shade":       {"400"},
	"Creepster":          {"400"},
	"Nosifer":             {"400"},
	"Faster One":         {"400"},
	"Monoton":            {"400"},
	"Press Start 2P":     {"400"},
	"Rubik Mono One":     {"400"},
	"Vast Shadow":        {"400"},
	"Fredericka the Great": {"400"},
	"Lobster":            {"400"},
	"Pacifico":           {"400"},
	"Righteous":          {"400"},
	"Permanent Marker":   {"400"},
	"Alfa Slab One":      {"400"},
	"Abril Fatface":      {"400"},
	"Playfair Display SC": {"400", "700", "900"},
	"Merriweather":       {"300", "400", "700", "900"},
	"Lora":               {"400", "500", "600", "700"},
	"Roboto Condensed":   {"300", "400", "700"},
	"Oswald":             {"200", "300", "400", "500", "600", "700"},
}

// FontWeightParam returns the weight option string for a font constructor.
// For non-variable fonts it returns ', weight: "400"' (or the appropriate weight).
// For variable fonts it returns an empty string.
func FontWeightParam(fontName string) string {
	if weights, ok := NonVariableFonts[fontName]; ok {
		// Use "400" if available, otherwise the first weight
		w := weights[0]
		for _, ww := range weights {
			if ww == "400" {
				w = "400"
				break
			}
		}
		return fmt.Sprintf(`, weight: "%s"`, w)
	}
	return ""
}

// getColor safely extracts a color hex value from a scheme map
func getColor(scheme map[string]any, key, fallback string) string {
	if v, ok := scheme[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// BuildGlobalsCSSTheme generates the globals.css content with Tailwind v4 @theme colors,
// body base styles, and mobile utilities when isMobile is true.
// Includes semantic tokens (foreground, muted, border, input, ring) derived from
// the color scheme so that shadcn-style utility classes work (bg-foreground,
// text-muted-foreground, border-border, etc.).
// Light theme only — no dark mode overrides.
func BuildGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any, isMobile bool) string {
	var sb strings.Builder

	fg := getColor(colorScheme, "text", "#111827")
	bg := getColor(colorScheme, "background", "#FFFFFF")
	surface := getColor(colorScheme, "surface", "#F3F4F6")
	primary := getColor(colorScheme, "primary", "#3B82F6")
	secondary := getColor(colorScheme, "secondary", "#6366F1")
	accent := getColor(colorScheme, "accent", "#F59E0B")

	// --- @import + @theme ---
	// Defines both the raw palette tokens AND the shadcn-style semantic tokens
	// (foreground, muted, border, card, destructive, *-foreground) that the
	// UI component templates (button, card, input, badge) and e-commerce
	// templates rely on.
	sb.WriteString(fmt.Sprintf(`@import "tailwindcss";

@theme {
  --color-primary: %s;
  --color-primary-foreground: %s;
  --color-secondary: %s;
  --color-secondary-foreground: %s;
  --color-accent: %s;
  --color-accent-foreground: %s;
  --color-background: %s;
  --color-surface: %s;
  --color-text: %s;
  --color-foreground: %s;
  --color-card: %s;
  --color-card-foreground: %s;
  --color-muted: %s;
  --color-muted-foreground: color-mix(in srgb, %s 60%%, %s);
  --color-border: color-mix(in srgb, %s 15%%, %s);
  --color-input: color-mix(in srgb, %s 15%%, %s);
  --color-ring: %s;
  --color-destructive: #DC2626;
  --color-destructive-foreground: #FFFFFF;
}
`,
		primary,
		bg,
		secondary,
		bg,
		accent,
		fg,
		bg,
		surface,
		fg,
		fg,
		bg,
		fg,
		surface,
		fg, bg,
		fg, bg,
		fg, bg,
		primary,
	))

	// --- @layer base: html + body ---
	sb.WriteString(`
@layer base {
  html {
    scroll-behavior: smooth;
    -webkit-text-size-adjust: 100%;
  }
  body {
    background-color: var(--color-background);
    color: var(--color-text);
    font-synthesis: none;
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
  }
`)

	// Mobile-specific base styles
	if isMobile {
		sb.WriteString(`  html {
    overscroll-behavior: none;
  }
  body {
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
    overscroll-behavior-y: contain;
    -webkit-tap-highlight-color: transparent;
    min-height: 100dvh;
  }
`)
	}

	sb.WriteString("}\n\n")

	// Mobile-specific utility classes for snap scroll and hidden scrollbar.
	// These are commonly needed by mobile apps (swipeable feeds, carousels,
	// game reels) and their absence causes layout bugs.
	if isMobile {
		sb.WriteString(`/* Mobile snap scroll & scrollbar utilities */
@layer utilities {
  .snap-y-mandatory {
    scroll-snap-type: y mandatory;
    overflow-y: auto;
  }
  .snap-x-mandatory {
    scroll-snap-type: x mandatory;
    overflow-x: auto;
  }
  .snap-start {
    scroll-snap-align: start;
  }
  .snap-center {
    scroll-snap-align: center;
  }
  .no-scrollbar::-webkit-scrollbar {
    display: none;
  }
  .no-scrollbar {
    -ms-overflow-style: none;
    scrollbar-width: none;
  }
}
`)
	}

	// Hide Next.js dev error overlay indicator in preview (must be outside @layer to win specificity)
	sb.WriteString(`nextjs-portal,
[data-nextjs-dialog-overlay],
[data-nextjs-toast] {
  display: none !important;
}
`)

	return sb.String()
}

// BuildEcommerceGlobalsCSSTheme generates the globals.css content for e-commerce projects.
// It extends the standard theme with e-commerce-specific utility classes and animations.
func BuildEcommerceGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any) string {
	base := BuildGlobalsCSSTheme(colorScheme, darkColorScheme, false)

	primary := getColor(colorScheme, "primary", "#3B82F6")

	return base + fmt.Sprintf(`
/* E-commerce utilities */
@layer utilities {
  .gradient-text {
    background: linear-gradient(135deg, var(--color-primary), var(--color-accent));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .glow-primary {
    box-shadow: 0 0 20px color-mix(in srgb, var(--color-primary) 30%%, transparent);
  }

  .glow-accent {
    box-shadow: 0 0 20px color-mix(in srgb, var(--color-accent) 30%%, transparent);
  }

  .glass {
    background: color-mix(in srgb, var(--color-surface) 80%%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
  }

  .glass-dark {
    background: color-mix(in srgb, var(--color-background) 60%%, transparent);
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
  }

  .grid-pattern {
    background-image: radial-gradient(circle, color-mix(in srgb, var(--color-text) 10%%, transparent) 1px, transparent 1px);
    background-size: 24px 24px;
  }
}

/* Custom scrollbar */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
::-webkit-scrollbar-track {
  background: transparent;
}
::-webkit-scrollbar-thumb {
  background: %s40;
  border-radius: 3px;
}
::-webkit-scrollbar-thumb:hover {
  background: %s60;
}

/* E-commerce animations */
@keyframes marquee {
  from { transform: translateX(0); }
  to { transform: translateX(-50%%); }
}
@keyframes float {
  0%%, 100%% { transform: translateY(0); }
  50%% { transform: translateY(-10px); }
}
@keyframes pulse-glow {
  0%%, 100%% { opacity: 0.6; }
  50%% { opacity: 1; }
}
@keyframes shimmer {
  0%% { background-position: -200%% 0; }
  100%% { background-position: 200%% 0; }
}

.animate-marquee { animation: marquee 30s linear infinite; }
.animate-float { animation: float 3s ease-in-out infinite; }
.animate-pulse-glow { animation: pulse-glow 2s ease-in-out infinite; }
.animate-shimmer {
  background: linear-gradient(90deg, transparent 25%%, color-mix(in srgb, var(--color-primary) 10%%, transparent) 50%%, transparent 75%%);
  background-size: 200%% 100%%;
  animation: shimmer 2s infinite;
}
`, primary, primary)
}

// BuildEcommerceLayout generates the root layout.tsx for e-commerce projects.
// It includes font imports, metadata, and wraps children with Providers, SiteHeader, SiteFooter, and Toaster.
func BuildEcommerceLayout(storeName string, typography map[string]any) string {
	if storeName == "" {
		storeName = "Store"
	}
	// Escape special characters that would break TypeScript string syntax
	storeName = strings.ReplaceAll(storeName, `\`, `\\`)
	storeName = strings.ReplaceAll(storeName, `"`, `\"`)

	headingFont := "Inter"
	bodyFont := "Inter"
	if typography != nil {
		if v, ok := typography["heading_font"].(string); ok && v != "" {
			headingFont = v
		}
		if v, ok := typography["body_font"].(string); ok && v != "" {
			bodyFont = v
		}
	}

	// Convert font names to import and variable names
	// e.g. "Space Grotesk" -> import "Space_Grotesk", var "spaceGrotesk"
	toImportName := func(name string) string {
		return strings.ReplaceAll(name, " ", "_")
	}
	toVarName := func(name string) string {
		parts := strings.Fields(name)
		if len(parts) == 0 {
			return "font"
		}
		if len(parts) == 1 {
			r := []rune(parts[0])
			r[0] = []rune(strings.ToLower(string(r[0])))[0]
			return string(r)
		}
		result := strings.ToLower(parts[0])
		for _, p := range parts[1:] {
			if len(p) > 0 {
				result += strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return result
	}

	headingImport := toImportName(headingFont)
	headingVar := toVarName(headingFont)
	bodyImport := toImportName(bodyFont)
	bodyVar := toVarName(bodyFont)

	sameFont := headingFont == bodyFont

	var fontImports string
	var fontConstructors string
	var bodyClassName string

	headingWeight := FontWeightParam(headingFont)
	bodyWeight := FontWeightParam(bodyFont)

	if sameFont {
		fontImports = fmt.Sprintf(`import { %s } from "next/font/google";`, headingImport)
		fontConstructors = fmt.Sprintf(`const %s = %s({ subsets: ["latin"], display: "swap"%s });`, headingVar, headingImport, headingWeight)
		bodyClassName = fmt.Sprintf("${%s.className}", headingVar)
	} else {
		fontImports = fmt.Sprintf(`import { %s, %s } from "next/font/google";`, headingImport, bodyImport)
		fontConstructors = fmt.Sprintf(`const %s = %s({ subsets: ["latin"], display: "swap", variable: "--font-heading"%s });
const %s = %s({ subsets: ["latin"], display: "swap"%s });`, headingVar, headingImport, headingWeight, bodyVar, bodyImport, bodyWeight)
		bodyClassName = fmt.Sprintf("${%s.variable} ${%s.className}", headingVar, bodyVar)
	}

	return fmt.Sprintf(`import type { Metadata } from "next";
%s
import { Toaster } from "sonner";
import { Providers } from "@/components/ecommerce/providers";
import { SiteHeader } from "@/components/ecommerce/site-header";
import { SiteFooter } from "@/components/ecommerce/site-footer";
import "./globals.css";

%s

export const metadata: Metadata = {
  title: "%s",
  description: "Welcome to %s — browse our collection and find what you love.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={`+"`"+`%s antialiased`+"`"+`}>
        <Providers>
          <SiteHeader />
          <main className="min-h-screen">{children}</main>
          <SiteFooter />
          <Toaster position="bottom-right" richColors />
        </Providers>
      </body>
    </html>
  );
}
`, fontImports, fontConstructors, storeName, storeName, bodyClassName)
}

// GetContent returns the hardcoded content for a template file.
// dataModels is used to generate the dynamic lib/schema.ts file.
func GetContent(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []models.DataModel) string {
	return GetContentWithLang(path, colorScheme, darkColorScheme, projectType, dataModels, "en")
}

// GetContentWithLang returns the hardcoded content for a template file, with a language code
// for localizing dynamic templates (not-found, error).
func GetContentWithLang(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []models.DataModel, lang string) string {
	// Dynamic templates that can't be static files
	switch path {
	case "app/globals.css":
		return BuildGlobalsCSSTheme(colorScheme, darkColorScheme, projectType == "mobile_app")
	case "app/not-found.tsx":
		return constants.GetNotFoundTemplate(lang)
	case "app/error.tsx":
		return constants.GetErrorTemplate(lang)
	case "lib/schema.ts":
		return database.BuildDrizzleSchema(dataModels)
	}
	// All other templates are static embedded files
	return GetTemplate(path)
}

// BuildEcommerceProviders returns the Providers component, conditionally including
// AuthProvider wrapping when needsAuth is true.
func BuildEcommerceProviders(needsAuth bool) string {
	if needsAuth {
		return `"use client";

import React from "react";
import { CartProvider } from "@/lib/wemob-store";
import { AuthProvider } from "@/lib/wemob-auth-client";

export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <AuthProvider>
      <CartProvider>{children}</CartProvider>
    </AuthProvider>
  );
}
`
	}
	return GetTemplate("components/ecommerce/providers.tsx")
}

// GetEcommerceContent returns the hardcoded content for an e-commerce template file.
// For components/ecommerce/providers.tsx, use BuildEcommerceProviders() instead when auth context is available.
// lang is used to localize {{t.key}} placeholders in the templates.
func GetEcommerceContent(path string, lang string) string {
	// No i18n needed for these
	if path == "lib/wemob-store.ts" || path == "components/ecommerce/empty-state.tsx" || path == "components/ecommerce/providers.tsx" {
		return GetTemplate(path)
	}
	raw := GetTemplate(path)
	if raw == "" {
		slog.Warn("GetEcommerceContent: no content for template path", "path", path)
		return ""
	}
	return constants.LocalizeEcommerceTemplate(raw, lang)
}

// GetUIContent returns the hardcoded content for a pre-built UI component template file.
func GetUIContent(path string) string {
	return GetTemplate(path)
}


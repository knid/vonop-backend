package constants

import "strings"

// notFoundStrings holds localized text for the 404 page.
var notFoundStrings = map[string][3]string{
	// [0] = "page not found", [1] = "back to home", [2] = "create with AI"
	"tr": {"Bu sayfa bulunamadı.", "Ana sayfaya dön", "Yapay Zeka ile Oluştur"},
	"en": {"This page could not be found.", "Back to home", "Create with AI"},
	"es": {"Esta página no se pudo encontrar.", "Volver al inicio", "Crear con IA"},
	"de": {"Diese Seite wurde nicht gefunden.", "Zurück zur Startseite", "Mit KI erstellen"},
	"fr": {"Cette page est introuvable.", "Retour à l'accueil", "Créer avec l'IA"},
	"pt": {"Esta página não foi encontrada.", "Voltar ao início", "Criar com IA"},
	"ar": {"لم يتم العثور على هذه الصفحة.", "العودة للرئيسية", "إنشاء بالذكاء الاصطناعي"},
}

// errorStrings holds localized text for the error boundary page.
var errorStrings = map[string][2]string{
	// [0] = "something went wrong", [1] = "try again"
	"tr": {"Bir şeyler ters gitti.", "Tekrar dene"},
	"en": {"Something went wrong.", "Try again"},
	"es": {"Algo salió mal.", "Intentar de nuevo"},
	"de": {"Etwas ist schiefgelaufen.", "Erneut versuchen"},
	"fr": {"Une erreur est survenue.", "Réessayer"},
	"pt": {"Algo deu errado.", "Tentar novamente"},
	"ar": {"حدث خطأ ما.", "حاول مرة أخرى"},
}

// GetNotFoundTemplate returns the localized 404 page template.
func GetNotFoundTemplate(lang string) string {
	s, ok := notFoundStrings[lang]
	if !ok {
		s = notFoundStrings["en"]
	}
	return `"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { usePathname } from "next/navigation"

export default function NotFound() {
  const pathname = usePathname()
  const [isEmbedded, setIsEmbedded] = useState(false)

  useEffect(() => {
    setIsEmbedded(window.self !== window.top)
  }, [])

  const handleCreateWithAI = () => {
    window.parent.postMessage(
      { type: "wemob-create-page", path: pathname },
      "*"
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <div className="text-center space-y-6 px-4">
        <div className="space-y-2">
          <h1 className="text-7xl font-bold text-foreground/10">404</h1>
          <p className="text-lg text-muted-foreground">` + s[0] + `</p>
          {pathname && (
            <p className="text-sm text-muted-foreground/60 font-mono">{pathname}</p>
          )}
        </div>
        <div className="flex flex-col items-center gap-3">
          {isEmbedded && (
            <button
              onClick={handleCreateWithAI}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 transition-colors"
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M8 2L9.5 6.5L14 8L9.5 9.5L8 14L6.5 9.5L2 8L6.5 6.5L8 2Z" fill="currentColor" />
              </svg>
              ` + s[2] + `
            </button>
          )}
          <Link
            href="/"
            className="text-sm text-muted-foreground hover:text-foreground transition-colors underline underline-offset-4"
          >
            ` + s[1] + `
          </Link>
        </div>
      </div>
    </div>
  )
}
`
}

// GetErrorTemplate returns the localized error boundary page template.
func GetErrorTemplate(lang string) string {
	s, ok := errorStrings[lang]
	if !ok {
		s = errorStrings["en"]
	}
	return `"use client"

import { useEffect } from "react"

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      <div className="text-center space-y-6 px-4">
        <div className="space-y-2">
          <h1 className="text-7xl font-bold text-foreground/10">Error</h1>
          <p className="text-lg text-muted-foreground">` + s[0] + `</p>
        </div>
        <button
          onClick={() => reset()}
          className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 transition-colors"
        >
          ` + s[1] + `
        </button>
      </div>
    </div>
  )
}
`
}

// GetGlobalErrorTemplate returns the localized global-error boundary template.
// global-error.tsx replaces the root layout when a runtime error bubbles past
// error.tsx, so it must render its own <html> and <body>. Uses inline styles
// because the root layout (and therefore global CSS) is not applied.
func GetGlobalErrorTemplate(lang string) string {
	s, ok := errorStrings[lang]
	if !ok {
		s = errorStrings["en"]
	}
	return `"use client"

import { useEffect } from "react"

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <html>
      <body style={{ margin: 0, fontFamily: "system-ui, -apple-system, sans-serif" }}>
        <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", background: "#ffffff" }}>
          <div style={{ textAlign: "center", padding: "2rem", maxWidth: "28rem" }}>
            <h1 style={{ fontSize: "4.5rem", fontWeight: 700, margin: 0, color: "rgba(0,0,0,0.1)", lineHeight: 1 }}>Error</h1>
            <p style={{ fontSize: "1.125rem", color: "rgba(0,0,0,0.6)", marginTop: "0.5rem", marginBottom: "1.5rem" }}>` + s[0] + `</p>
            <button
              onClick={() => reset()}
              style={{
                padding: "0.625rem 1.25rem",
                borderRadius: "0.5rem",
                border: "none",
                background: "#111111",
                color: "#ffffff",
                fontSize: "0.875rem",
                fontWeight: 500,
                cursor: "pointer",
              }}
            >
              ` + s[1] + `
            </button>
          </div>
        </div>
      </body>
    </html>
  )
}
`
}

// DetectLanguage infers the primary language from a user prompt by checking
// for language-specific characters and common words. Returns an ISO 639-1 code.
//
// The earlier version relied purely on diacritics, which misfires badly on
// short Turkish prompts — "el kremi satacagim" has zero Turkish-specific
// characters and got classified as English, shipping a Turkish merchant an
// English storefront. The word-level pass below catches the common case.
func DetectLanguage(prompt string) string {
	lower := strings.ToLower(prompt)

	// Turkish-specific characters
	if strings.ContainsAny(prompt, "ğışçöüĞİŞÇÖÜ") {
		return "tr"
	}
	// Arabic script
	for _, r := range prompt {
		if r >= 0x0600 && r <= 0x06FF {
			return "ar"
		}
	}
	// Spanish-specific patterns
	if strings.ContainsAny(prompt, "ñ¿¡") {
		return "es"
	}
	// German-specific
	if strings.Contains(lower, "ß") || (strings.Contains(lower, "ü") && strings.Contains(lower, "ö")) {
		return "de"
	}
	// French-specific patterns
	if strings.Contains(lower, "ç") && (strings.Contains(lower, "é") || strings.Contains(lower, "è")) {
		return "fr"
	}
	// Portuguese
	if strings.Contains(lower, "ã") || strings.Contains(lower, "õ") {
		return "pt"
	}

	// Word-level fallback: look for high-signal ASCII-only tokens.
	if lang := detectByWords(lower); lang != "" {
		return lang
	}

	return "en"
}

// detectByWords scans a lowercased prompt for common words that strongly
// suggest a particular language. Only tokens that are rare in English and
// common in the target language qualify — otherwise every store whose name
// happens to contain "bir" or "en" gets misclassified. Returns "" when
// nothing matches.
func detectByWords(lower string) string {
	// Tokenise on whitespace and common punctuation so suffix-heavy languages
	// like Turkish match agglutinated forms ("satacagim", "istiyorum").
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == '!' || r == '?' || r == ';' || r == ':' || r == '"' || r == '\''
	})
	words := make(map[string]bool, len(fields))
	for _, f := range fields {
		words[f] = true
	}

	turkish := []string{
		// High-signal agglutinated verb forms — "-acagim"/"-ecegim" (I will),
		// "-iyorum" (I am -ing), "-mak"/"-mek" infinitives. These never appear
		// in English prose and catch short prompts without diacritics.
		"istiyorum", "yapacagim", "yapmak", "satacagim", "satmak", "satiyorum",
		"olacak", "olsun", "acmak", "acacagim", "kurmak", "kuracagim",
		// Ultra-common function words — single matches aren't enough on
		// their own, but combined with the verb forms above they're decisive.
		"icin", "cok", "bir", "ile", "degil", "olan", "lazim", "var", "yok",
		// Domain words frequent in store prompts.
		"magaza", "urun", "satis", "kargo", "sepet", "siparis",
	}
	for _, w := range turkish {
		if words[w] {
			return "tr"
		}
	}

	// Space for future additions (es/de/fr/pt without diacritics) — keep
	// the pattern here rather than growing DetectLanguage.
	return ""
}

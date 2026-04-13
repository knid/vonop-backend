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

// DetectLanguage infers the primary language from a user prompt by checking
// for language-specific characters and common words. Returns an ISO 639-1 code.
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

	return "en"
}

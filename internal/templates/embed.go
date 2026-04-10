package templates

import (
	"embed"
	"io/fs"
)

//go:embed all:nextjs
var nextjsFS embed.FS

// GetTemplate reads a template file by its generated-project-relative path.
// For example: "package.json", "components/ecommerce/product-card.tsx", "lib/utils.ts".
// The ".gitignore" path is mapped to ".gitignore.tmpl" on disk to avoid git interference.
// Returns the file content as a string, or empty string if not found.
func GetTemplate(path string) string {
	fsPath := "nextjs/" + path
	if path == ".gitignore" {
		fsPath = "nextjs/.gitignore.tmpl"
	}
	data, err := nextjsFS.ReadFile(fsPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// FS returns the embedded filesystem rooted at nextjs/.
func FS() fs.FS {
	sub, _ := fs.Sub(nextjsFS, "nextjs")
	return sub
}

// HasTemplate reports whether a template file exists at the given path.
func HasTemplate(path string) bool {
	fsPath := "nextjs/" + path
	if path == ".gitignore" {
		fsPath = "nextjs/.gitignore.tmpl"
	}
	_, err := nextjsFS.ReadFile(fsPath)
	return err == nil
}

// Template file paths that are managed by hardcoded templates, not LLM-generated.
var BaseFilePaths = []string{
	"package.json",
	"tsconfig.json",
	"postcss.config.mjs",
	"next.config.mjs",
	"Dockerfile",
	"app/globals.css",
	"app/not-found.tsx",
	"app/error.tsx",
	".gitignore",
}

// DBFilePaths are template-managed files injected only after successful Neon provisioning.
var DBFilePaths = []string{
	"lib/db.ts",
	"lib/schema.ts",
}

// EcommerceFilePaths are template-managed files injected only for e-commerce projects.
var EcommerceFilePaths = []string{
	"lib/wemob-store.ts",
	"components/ecommerce/product-card.tsx",
	"components/ecommerce/cart-drawer.tsx",
	"components/ecommerce/price-display.tsx",
	"components/ecommerce/quantity-selector.tsx",
	"components/ecommerce/product-gallery.tsx",
	"components/ecommerce/checkout-form.tsx",
	"components/ecommerce/category-nav.tsx",
	"components/ecommerce/cart-item-row.tsx",
	"components/ecommerce/order-summary.tsx",
	"components/ecommerce/empty-state.tsx",
	"components/ecommerce/review-list.tsx",
	"components/ecommerce/wishlist-button.tsx",
	"components/ecommerce/site-header.tsx",
	"components/ecommerce/site-footer.tsx",
	"components/ecommerce/providers.tsx",
	"components/ecommerce/newsletter-section.tsx",
	"app/cart/page.tsx",
	"app/checkout/page.tsx",
	"app/checkout/success/page.tsx",
	"app/checkout/cancel/page.tsx",
	"app/products/page.tsx",
	"app/products/[slug]/page.tsx",
	"app/categories/[slug]/page.tsx",
	"app/policies/shipping/page.tsx",
	"app/policies/returns/page.tsx",
	"app/policies/privacy/page.tsx",
	"app/policies/terms/page.tsx",
}

// UIFilePaths are pre-built UI component templates injected during initial generation.
var UIFilePaths = []string{
	"lib/utils.ts",
	"components/ui/button.tsx",
	"components/ui/card.tsx",
	"components/ui/input.tsx",
	"components/ui/badge.tsx",
	"components/ui/skeleton.tsx",
}

// IsTemplateFile checks if a file path is managed by any template category.
func IsTemplateFile(path string) bool {
	for _, paths := range [][]string{BaseFilePaths, DBFilePaths, EcommerceFilePaths, UIFilePaths} {
		for _, tp := range paths {
			if path == tp {
				return true
			}
		}
	}
	return false
}

// AllTemplatePaths returns a deduplicated slice of all template file paths.
func AllTemplatePaths() []string {
	seen := make(map[string]bool)
	var all []string
	for _, paths := range [][]string{BaseFilePaths, DBFilePaths, EcommerceFilePaths, UIFilePaths} {
		for _, p := range paths {
			if !seen[p] {
				seen[p] = true
				all = append(all, p)
			}
		}
	}
	return all
}

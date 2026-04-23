package templates

import (
	"strings"
	"testing"
)

func TestGetTemplateSmoke(t *testing.T) {
	checks := []struct {
		path     string
		contains string
	}{
		{"package.json", `"next": "^16.0.0"`},
		{"components/ui/button.tsx", "buttonVariants"},
		{"components/ecommerce/product-card.tsx", "ProductCard"},
		{"lib/db.ts", "drizzle"},
		{"lib/wemob-store.ts", "CartProvider"},
		{"lib/wemob-store-server.ts", "server-only"},
		{"lib/schema/index.ts", "pgTable"},
		{".gitignore", "node_modules"},
		{"lib/utils.ts", "twMerge"},
		{"app/cart/page.tsx", "getCart"},
		{"app/api/checkout/route.ts", "getAdapter"},
		{"app/api/cart/route.ts", "wemob_cart_session"},
		{"lib/payments/stripe.ts", "stripe"},
		{"components/ecommerce/site-header.tsx", "SiteHeader"},
		{"app/policies/shipping/page.tsx", "shipping"},
		{"components/ecommerce/checkout-form.tsx", "/api/checkout"},
	}
	for _, c := range checks {
		content := GetTemplate(c.path)
		if content == "" {
			t.Errorf("GetTemplate(%q) returned empty", c.path)
		} else if !strings.Contains(content, c.contains) {
			t.Errorf("GetTemplate(%q) missing %q", c.path, c.contains)
		}
	}
}

func TestIsTemplateFile(t *testing.T) {
	yes := []string{"package.json", "components/ecommerce/product-card.tsx", "lib/utils.ts"}
	for _, p := range yes {
		if !IsTemplateFile(p) {
			t.Errorf("IsTemplateFile(%q) should be true", p)
		}
	}
	no := []string{"app/globals.css.bak", "random.tsx", ""}
	for _, p := range no {
		if IsTemplateFile(p) {
			t.Errorf("IsTemplateFile(%q) should be false", p)
		}
	}
}

func TestAllTemplatePaths(t *testing.T) {
	all := AllTemplatePaths()
	if len(all) < 30 {
		t.Errorf("AllTemplatePaths() returned %d, expected 30+", len(all))
	}
}

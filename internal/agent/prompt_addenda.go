package agent

import (
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// EcommerceAddendum is appended to BasePromptWeb for project.ProjectType == "ecommerce".
// When the project also has has_database=true, DatabaseAddendum fires in
// addition; this file intentionally overrides the DatabaseAddendum's
// lib/db/schema.ts and lib/db/queries.ts path claims for ecommerce projects.
const EcommerceAddendum = `# ECOMMERCE ADDENDUM
You are building a fully functional storefront, not a marketing landing page.
Every project has a browsable product catalog, product detail pages, a cart,
a checkout flow, and order confirmation pages. If you only build a homepage,
the project fails validation.

## Architecture
The tenant gets a dedicated Postgres (Neon) database. On the very first
provision_database call, wemob auto-seeds the canonical ecommerce schema:
stores, products, product_categories, discounts, carts, cart_items, orders,
order_items, product_reviews, wishlists. After that first call, DATABASE_URL
is injected, has_database flips to true, and you must NOT call
provision_database again for this project.

On an ecommerce project's first turn, call provision_database once to trigger
the seed — do not pass a custom data_models argument; the canonical schema is
loaded automatically. Treat subsequent runs as "database already seeded".

Prices are stored in cents (bigint). All rows use UUID primary keys. Stores,
products, categories, and discounts use soft deletes (deleted_at IS NULL).

Do not drop or rename the core columns seeded by the starter schema — the
template checkout, cart, and listing code depends on them. Prefer adding new
nullable columns or new tables when extending the schema.

## Template Files (Do Not Recreate, Do Not Rewrite)
These paths are template-managed. Import from them; never overwrite them:

Data layer:
- lib/db.ts                     drizzle-neon-http client
- lib/schema/index.ts           drizzle schema mirroring the seeded tables
- lib/wemob-store.ts            client SDK (hooks + write helpers, "use client")
- lib/wemob-store-server.ts     server-only drizzle reads (import "server-only")
- lib/wemob-store-types.ts      shared type declarations

Payments:
- lib/payments/index.ts, lib/payments/stripe.ts, lib/payments/iyzico.ts,
  lib/payments/cod.ts

API routes (local /api/* backed by drizzle):
- app/api/cart/route.ts, app/api/cart/[itemId]/route.ts
- app/api/checkout/route.ts
- app/api/discount/route.ts
- app/api/reviews/route.ts
- app/api/wishlist/route.ts, app/api/wishlist/[productId]/route.ts
- app/api/webhooks/stripe/route.ts, app/api/webhooks/iyzico/route.ts

Pages:
- app/cart/page.tsx, app/checkout/page.tsx,
  app/checkout/success/page.tsx, app/checkout/cancel/page.tsx
- app/products/page.tsx, app/products/[slug]/page.tsx,
  app/categories/[slug]/page.tsx
- app/policies/shipping|returns|privacy|terms/page.tsx

Components (all of components/ecommerce/*):
- ProductCard, CartDrawer, CartView, CartItemRow, PriceDisplay,
  QuantitySelector, ProductGallery, ProductsBrowser, ProductDetailActions,
  CategoryProductsGrid, CheckoutForm, CategoryNav, OrderSummary, EmptyState,
  ReviewList, WishlistButton, SiteHeader, SiteFooter, Providers,
  NewsletterSection

## Pages You Build
You own only these top-level pages:
- app/page.tsx: hero, featured products, category highlights, promo content.
  Render on the server — import getProducts / getCategories from
  '@/lib/wemob-store-server' directly (no CartProvider wrapping needed; the
  root layout already mounts Providers).
- app/about/page.tsx: brand story and mission tailored to the store concept.
- app/contact/page.tsx: contact form or details.
- app/faq/page.tsx: 8-12 FAQs relevant to the store.

Never hardcode product, category, or price arrays. Any list of objects with
name, price, or image must come from the data layer. FAQ items, testimonials,
team members, and navigation links may still be hardcoded.

## Data Access
Server components and route handlers read from '@/lib/wemob-store-server':
  getStoreConfig, getProducts, getProduct, getCategories, getCart,
  getOrder, getProductReviews, getWishlist.
These return plain objects ready to render; they run drizzle internally.

Client components ("use client") read and write via '@/lib/wemob-store':
  hooks useCart, useProducts, useProduct, useCategories, useStoreConfig,
  useReviews, useWishlist; plus addCartItem, updateCartItem, removeCartItem,
  applyDiscountCode, removeDiscount, submitReview, addToWishlist,
  removeFromWishlist. The client functions POST to the template's local
  /api/* routes, which do the actual drizzle writes.

Never import drizzle or '@/lib/db' in new code — route through the helper
modules above. Pages reading from the server helpers must export
  export const dynamic = "force-dynamic"
so Next.js does not cache stale data.

## Schema and Data Management
Use execute_sql for everything SQL-shaped:
- readonly=true  for one-off SELECTs while exploring data.
- readonly=false for bulk INSERTs, UPDATEs, CREATE TABLE, ALTER TABLE.

When the user asks to add products, categories, discounts, or change store
settings, run INSERT / UPDATE statements against the live tables via
execute_sql. The storefront picks up new rows automatically — no component
edits needed. Generate product imagery with the generate_image tool first and
embed the returned URLs in the images jsonb array.

After adding new tables or columns with execute_sql, update lib/schema/index.ts
so the drizzle types match the live database.

## Prices and Images
Prices live on products as price_in_cents and compare_at_price. Always format
with formatPrice from '@/lib/wemob-store'. Never divide by 100 or call
toFixed. Show a line-through original price alongside the highlighted sale
price when compare_at_price is greater than price_in_cents. Show an
out-of-stock badge when in_stock is false.

Products already have hosted images in their images array — render
product.images[0] with next/image. For hero, category header, and about-page
imagery, call the generate_image tool. Never use picsum.photos, Unsplash, or
any external stock URL, and never draw SVG illustrations of real-world objects.

## Enabling Checkout Providers
Stripe / Iyzico / COD are wired in lib/payments/. For a provider to appear in
the checkout form, two things must be true:
1. The store owner has connected the provider in the wemob dashboard, which
   populates the AppIntegration row and injects the API keys at deploy time.
2. The tenant stores.payment_config jsonb has the provider enabled, shaped
   like {"stripe": {"enabled": true}} or {"iyzico": {"enabled": true}}. The
   getStoreConfig helper reads this jsonb and exposes payment_providers to
   the checkout UI.

When the user asks for checkout to work with a specific provider, remind them
to connect the provider in the dashboard AND update stores.payment_config
via execute_sql. COD needs only the payment_config flag.
`

// AuthAddendum is appended to the base prompt when project.Settings.needs_auth is true.
const AuthAddendum = `# AUTH ADDENDUM
The project ships with a server-only session helper library at
lib/auth/session.ts. Read lib/auth/types.ts before assuming anything about
the user shape. The baseline AuthUser is { id, email, name }; do not invent
extra fields.

## Server-Side Session Reads
- In Server Components and server-side route handlers, call getSession() from
  lib/auth/session.ts. It reads the session cookie internally and takes no
  arguments. Never pass the request object.
- Protected pages check the session at the top of the page component and
  redirect to /login with the redirect() helper from next/navigation when
  the session is null.
- Session data is nested: session.user.id, session.user.email. Never access
  session.userId or session.email directly.

## Server Actions
- Signup and login are Server Actions. They hash passwords, set the session
  cookie, and return { success, user } on success or { success: false, error }
  on failure. The client uses the returned user immediately; do not refetch
  the session right after a successful login (the Set-Cookie header may not
  yet be visible to the next request).
- Logout is also a Server Action; it clears the cookie and redirects.

## Client Rules
- Never put auth logic on the client. No direct password hashing, no cookie
  writes, no JWT parsing in "use client" files.
- A client AuthProvider may cache the current user for UI rendering, but the
  source of truth is always the server session.
- Guard client redirects with an isLoading flag so unauthenticated UI does
  not flash before the session resolves.
`

// buildAIAddendum is appended to the base prompt when project.Settings.has_ai is
// true. It confirms the Wemob AI Gateway is already wired up so subsequent
// runs and chat turns use the existing helper instead of calling
// setup_wemob_ai_gateway again.
//
// The chat model list is rendered dynamically from the user's allowed set so
// the LLM never recommends a model the gateway would 403 at runtime. When no
// deps callback is available (e.g. standalone tooling), the block falls back
// to a conservative "the setup tool will list available models" line.
func buildAIAddendum(deps *RunDependencies) string {
	allowedChat := gatewayAllowedChatModels(deps)

	modelBlock := "Available chat models will be listed when you call\nsetup_wemob_ai_gateway — use whatever that tool reports."
	defaultModel := constants.GatewayDefaultChatModel
	if len(allowedChat) > 0 {
		modelBlock = "Available chat models on the user's current plan (pass as the\nmodel field on wemobAI().chat.completions.create — anything else\nwill 403 at runtime):\n" + renderGatewayChatModelsBlock(allowedChat)
		defaultModel = defaultGatewayChatModel(allowedChat)
	}

	return `# AI ADDENDUM
This project has the built-in Wemob AI Gateway wired up. In every user-facing
message, call it "AI" or "yapay zeka" — never "Wemob Gateway", "OpenRouter",
"Claude", "GPT", "Anthropic", or any underlying model name. The user does not
care what powers it.

The AI helper lives at lib/wemob-ai.ts and exports a preconfigured OpenAI
client plus a thin image helper:

    import { wemobAI, generateImage } from '@/lib/wemob-ai';

- wemobAI() returns an OpenAI client pointed at the Wemob Gateway. Use it
  exactly like the real OpenAI SDK — chat, streaming, tools, tool_choice,
  response_format, seed, and vision all pass through to the upstream.
- generateImage({ model, prompt, imageSize, numImages }) returns string[] of
  public image URLs. Use it for AI-generated imagery the user requests at
  runtime (not for placeholder hero art — use the generate_image tool for that).

` + modelBlock + `

Available image models: fal-ai/flux/schnell (default), fal-ai/flux-pro/v1.1,
fal-ai/flux-pro/v1.1-ultra.

The openai npm package is already in package.json — do not add it again, and
do not add @anthropic-ai/sdk, @google/generative-ai, or any other AI SDK. They
are redundant with wemobAI().

## Server-Side Only
WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL are auto-injected — do not add
them yourself, do not ask the user for an API key, do not surface a model
picker. NEVER import @/lib/wemob-ai from a "use client" file. The key must
never reach the browser.

## Patterns
- One-shot generation: a Server Action or route handler calls
    const res = await wemobAI().chat.completions.create({
      model: '` + defaultModel + `',
      messages,
    });
    return res.choices[0]?.message?.content ?? '';
- Streaming chat: app/api/chat/route.ts opens
    const stream = await wemobAI().chat.completions.create({
      model: '` + defaultModel + `', messages, stream: true,
    });
  and streams ` + "`chunk.choices[0]?.delta?.content`" + ` to a Response built from
  a ReadableStream. The client fetches '/api/chat' and appends tokens to state.
- Tool calling / structured output: pass ` + "`tools`" + `, ` + "`tool_choice`" + `, or
  ` + "`response_format`" + ` directly to chat.completions.create — the gateway
  forwards them untouched.
- Vision: models tagged [vision] in the list above accept image_url content
  parts exactly like the OpenAI SDK. Send images as data URLs or public URLs.
- Image generation: a Server Action calls generateImage({ prompt }) and
  returns the URL array. Render the first URL with next/image.

## Do Not
- Do NOT scaffold OPENAI_API_KEY, ANTHROPIC_API_KEY, OPENROUTER_API_KEY,
  GOOGLE_API_KEY, or any other third-party AI key. Billing already runs
  through the user's Wemob plan.
- Do NOT call setup_wemob_ai_gateway again — the helper already exists.
- Do NOT rewrite lib/wemob-ai.ts. It is template-managed; extend it only by
  adding new functions, never by replacing the existing exports.
`
}

// AttachmentsAddendum builds the system-prompt section that lists every
// non-deleted asset in the project (capped at the controller layer). The
// LLM uses this to know what files exist, embed URLs in generated app code,
// and call read_attachment with stable IDs. Returns "" when the pool is
// empty so the prompt doesn't carry a useless header.
func AttachmentsAddendum(pool []models.AppAsset) string {
	if len(pool) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Project Assets\n\n")
	sb.WriteString("You have access to the following files uploaded to this project. ")
	sb.WriteString("Reference them by URL in any code you generate. ")
	sb.WriteString("To read the contents of a non-image file, call the `read_attachment` tool with its ID. ")
	sb.WriteString("Image attachments sent on the current message are already visible to you — no tool call needed.\n\n")
	sb.WriteString("| ID | Name | Type | Size | URL |\n")
	sb.WriteString("|----|------|------|------|-----|\n")
	for _, a := range pool {
		sb.WriteString("| ")
		sb.WriteString(a.ID.String())
		sb.WriteString(" | ")
		sb.WriteString(a.FileName)
		sb.WriteString(" | ")
		sb.WriteString(a.ContentType)
		sb.WriteString(" | ")
		sb.WriteString(humanReadableBytes(a.FileSize))
		sb.WriteString(" | ")
		sb.WriteString(a.PublicURL)
		sb.WriteString(" |\n")
	}
	return sb.String()
}

// humanReadableBytes formats a byte count as KB / MB for display in the
// manifest. Whole-number-only — exact precision isn't useful to the LLM.
func humanReadableBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%d MB", n/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%d KB", n/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// DatabaseAddendum is appended to the base prompt when project.Settings.has_database is true.
const DatabaseAddendum = `# DATABASE ADDENDUM
This project has a built-in wemob database — a serverless database that wemob
provisions and manages automatically. In every user-facing message, call it
"wemob db", "your database", or just "the database". NEVER mention Neon,
Postgres, Drizzle, Supabase, Firebase, or any backing technology in
conversational text. The user does not care what powers it.

The database is reachable via process.env.DATABASE_URL. The schema lives in
lib/db/schema.ts (Drizzle) and reusable queries live in lib/db/queries.ts.
The database is pre-seeded with realistic demo data including AI-generated
images. Do not create seed routes (app/api/seed/*), seed buttons, or
"load demo data" UI. Image columns hold real hosted URLs; render them directly.

## How to Query
- Import { db } from '@/lib/db' and your tables from '@/lib/db/schema'.
  Import operators from 'drizzle-orm': eq, and, desc, sql, etc.
- Use db.select / insert / update / delete in Server Components and route
  handlers. Never write raw SQL inside TSX.
- Drizzle uses camelCase property names even when the underlying column is
  snake_case (row.userId, row.createdAt, row.passwordHash).
- UUID columns reject non-UUID strings. For placeholder user ids use
  '00000000-0000-0000-0000-000000000000', never 'guest'.
- Any page.tsx that reads from '@/lib/db' must export
  export const dynamic = "force-dynamic" so Next.js does not cache stale data.
- lib/db/schema.ts and lib/db/queries.ts are template-managed scaffolding:
  extend them, do not replace them.

## Schema Changes and Ad-Hoc Queries
Use the execute_sql tool for anything that is not a compiled Drizzle query:
- readonly=true for one-off SELECTs while exploring the data.
- readonly=false for CREATE TABLE, ALTER TABLE, DROP TABLE, and for seed
  INSERTs you need to run yourself.

After altering the schema with execute_sql, update lib/db/schema.ts so the
Drizzle types match the live tables. If the seed data references imagery,
generate the images with the generate_image tool first and store the
returned URLs in the INSERT statements.
`

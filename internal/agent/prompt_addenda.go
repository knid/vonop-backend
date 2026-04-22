package agent

import (
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// EcommerceAddendum is appended to BasePromptWeb for project.ProjectType == "ecommerce".
const EcommerceAddendum = `# ECOMMERCE ADDENDUM
You are building a fully functional e-commerce application, not a marketing
landing page. It has a browsable product catalog, product detail pages, a
shopping cart, a checkout flow, and order confirmation. If you only build a
homepage, the project will fail validation.

## Store SDK
All product, category, cart, and order data comes from the store SDK at
lib/store-sdk.ts. The store backend is pre-provisioned. Key functions:
fetchProducts, fetchProductBySlug, fetchCategories, searchProducts, plus cart
and checkout helpers exposed via the CartProvider. Files importing from the
SDK must start with "use client" (the SDK is client-only).

Do not create lib/db.ts, lib/schema.ts, or any app/api/ routes for store data.
Never hardcode product, category, or price data. Any array of objects with
name, price, or image fields must come from the SDK, not a literal. FAQ items,
testimonials, team members, and navigation links may still be hardcoded.

## Template Files (Do Not Recreate)
The following live in components/ecommerce/ and app/ as template files. Import
and use them directly, never rewrite them:
- ProductCard, CartDrawer, PriceDisplay, QuantitySelector, ProductGallery,
  CheckoutForm, CategoryNav, CartItemRow, OrderSummary, EmptyState,
  ReviewList, WishlistButton, SiteHeader, SiteFooter, Providers,
  NewsletterSection
- app/layout.tsx, app/globals.css
- app/products/page.tsx, app/products/[slug]/page.tsx,
  app/categories/[slug]/page.tsx
- app/cart/page.tsx, app/checkout/page.tsx,
  app/checkout/success/page.tsx, app/checkout/cancel/page.tsx
- app/policies/shipping/page.tsx, app/policies/returns/page.tsx,
  app/policies/privacy/page.tsx, app/policies/terms/page.tsx

## Pages You Build
You are responsible for the homepage and the informational pages:
- app/page.tsx: hero, featured products via fetchProducts, category highlights
  via fetchCategories, promotional content.
- app/about/page.tsx: brand story and mission tailored to the store concept.
- app/contact/page.tsx: contact form or details.
- app/faq/page.tsx: 8 to 12 FAQs relevant to the store.

## Prices and Images
Product prices live on the product itself: price_in_cents and compare_at_price.
Always format currency via formatPrice. Never divide by 100 or call toFixed.
Show a line-through original price alongside a highlighted sale price when
compare_at_price is greater than price_in_cents. Show an out of stock badge
when in_stock is false.

Products already have real images in their images array. Use next/image with
product.images[0]. For hero, category header, and about imagery, call the
generate_image tool. Never use picsum.photos, Unsplash, or any external stock
URL, and never draw SVG illustrations of real-world objects.

## Store Data Tools
When the user asks to add, change, or remove products, categories, discounts,
or store settings, use the store tools (create_product, bulk_create_products,
list_products, update_product, delete_product, create_category, list_categories,
update_category, delete_category, create_discount, list_discounts,
update_discount, delete_discount, get_store, update_store_settings) rather than
editing code or hardcoding data in TSX. The store SDK on the frontend will
pick up the new data automatically — you do not need to change components to
render newly-added products. Call list_* / get_store before any update or
delete so you're working with real ids and slugs.
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

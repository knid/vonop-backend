package agent

import (
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// EcommerceAddendum is appended to BasePromptWeb for project.ProjectType == "ecommerce".
// Ecommerce projects bypass DatabaseAddendum — this addendum is the single
// source of truth for their data-layer story.
const EcommerceAddendum = `# ECOMMERCE ADDENDUM
You are building a fully functional storefront, not a marketing landing page.
Every project has a browsable product catalog, product detail pages, a cart,
a checkout flow, and order confirmation pages. If you only build a homepage,
the project fails validation.

## Architecture
The tenant's Postgres database is provisioned and seeded at project-creation
time with the canonical ecommerce schema. DATABASE_URL is already injected
and the schema is already live — do NOT call provision_database. The DB is
scoped to exactly one store, so there is NO store_id / tenant_id column;
store-wide settings live on a single-row ` + "`config`" + ` table.

Tables seeded: config, product_categories, products, discounts, carts,
cart_items, orders, order_items, product_reviews, wishlists, contact_messages.
Read lib/schema/index.ts for the exact columns and types.

For store-wide settings (name, currency, locale, tax_rate, payment_config),
UPDATE the single ` + "`config`" + ` row — never INSERT. Example:
    UPDATE config SET store_name = 'Acme', currency = 'EUR',
      tax_rate = 8,
      payment_config = '{"stripe": {"enabled": true}}'::jsonb;
` + "`tax_rate`" + ` is a percentage (8 means 8%), not a fraction.

Prices are in cents (bigint). UUIDs are primary keys. Products, categories,
and discounts soft-delete via deleted_at IS NULL.

Do not drop or rename seeded columns — the template checkout/cart/listing
code depends on them. Extend with new nullable columns or new tables.

## Template files
Template-managed; import from them, do not overwrite. Read lib/schema/index.ts
and components/ecommerce/* for the exact shapes you're working with.

- app/layout.tsx, app/globals.css — root shell; edit_file only
- lib/db.ts, lib/schema/index.ts — data layer
- lib/wemob-store.ts (client SDK), lib/wemob-store-server.ts (server reads),
  lib/wemob-store-types.ts, lib/discounts.ts
- lib/payments/* — stripe / iyzico / cod adapters
- app/api/cart, checkout, discount, reviews, wishlist, contact, webhooks/* — all backed by drizzle
- app/cart, app/checkout{,/success,/cancel}, app/products{,/[slug]},
  app/categories/[slug], app/policies/{shipping,returns,privacy,terms}
- components/ecommerce/* — ProductCard, CartDrawer, CheckoutForm, SiteHeader,
  SiteFooter, Providers, and ~15 more

## Pages you build
Only these top-level pages are yours:
- app/page.tsx — hero, featured products, category highlights. Render on the
  server; import getProducts/getCategories from '@/lib/wemob-store-server'.
- app/about/page.tsx, app/contact/page.tsx, app/faq/page.tsx

Never hardcode product, category, or price arrays. FAQ items, testimonials,
team members, and navigation links may be hardcoded.

## Data access
Server components + route handlers: import from '@/lib/wemob-store-server'
(getStoreConfig, getProducts, getProduct, getCategories, getCart, getOrder,
getProductReviews, getWishlist).

Client components ("use client"): import from '@/lib/wemob-store' (hooks
useCart, useProducts, useProduct, useCategories, useStoreConfig, useReviews,
useWishlist; mutation helpers addCartItem, applyDiscountCode, submitReview,
etc.). Reads use server actions from '@/lib/wemob-storefront-actions'; writes
POST to /api/* routes.

Do not import drizzle or '@/lib/db' in pages — route through the helpers.
Pages that read from server helpers must export
  export const dynamic = "force-dynamic"
so Next.js does not cache.

## Schema / data management
For the initial store seed (first turn), call setup_store ONCE with the
store config, categories, and products in a single structured payload.
It runs config UPDATE + category INSERTs + product INSERTs in one
transaction; categories reference each other by parent_slug and products
reference categories by category_slug (both resolved server-side). Do NOT
follow up setup_store with execute_sql INSERTs for the same data — it's a
one-shot tool. Generate product and category imagery with generate_image
before calling setup_store and pass the URLs in the images array / image
field.

For everything after the initial seed (one-off edits, adding a single
product, schema changes, ad-hoc SELECTs), use execute_sql:
- readonly=true for one-off SELECTs while exploring
- readonly=false for INSERT/UPDATE, CREATE TABLE, ALTER TABLE

UUIDs come back as 36-char strings — safe to quote in follow-up SQL. Append
RETURNING to capture inserted ids without a round-trip. Writes run in a
transaction; a failure rolls back the whole statement block.

After ALTER TABLE or new tables, update lib/schema/index.ts so drizzle types
match the live database.

## Prices and images
Always format with formatPrice from '@/lib/wemob-store' — never divide by 100.
Show a line-through compare_at_price when it's greater than price_in_cents.
Show out-of-stock when in_stock is false.

Products ship with hosted images in their images array; render images[0]
with next/image. For hero / category header / about imagery, call
generate_image. Never use picsum.photos, Unsplash, or any stock URL.

## i18n
Storefront chrome is localised at template-copy time. By the time you read a
template file, every {{t.xxx}} / {{te.xxx}} placeholder is already resolved.
If you still see one, stop — the localiser broke; report it rather than
hand-writing a replacement.

For LLM-authored pages (home, about, contact, FAQ), write directly in the
store's language.

## Checkout providers
Stripe / Iyzico / COD are wired in lib/payments/. For a provider to appear
in the checkout form: (1) the merchant connects it in the wemob dashboard
(keys injected at deploy), and (2) config.payment_config has it enabled,
e.g. '{"stripe": {"enabled": true}}'. Update both when the user asks for a
specific provider.
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

// DatabaseAddendum is appended to the base prompt for non-ecommerce projects
// with has_database=true. Ecommerce projects take the EcommerceAddendum path,
// which carries its own (tighter) database instructions — this addendum is
// skipped for them to avoid conflicting schema paths.
const DatabaseAddendum = `# DATABASE ADDENDUM
This project has a built-in wemob database — a serverless database that wemob
provisions and manages automatically. In every user-facing message, call it
"wemob db", "your database", or just "the database". NEVER mention Neon,
Postgres, Drizzle, Supabase, Firebase, or any backing technology in
conversational text. The user does not care what powers it.

The database is reachable via process.env.DATABASE_URL. The schema lives in
lib/schema.ts (Drizzle). The database is pre-seeded with realistic demo data
including AI-generated images. Do not create seed routes (app/api/seed/*),
seed buttons, or "load demo data" UI. Image columns hold real hosted URLs;
render them directly.

## How to Query
- Import { db } from '@/lib/db' and your tables from '@/lib/schema'.
  Import operators from 'drizzle-orm': eq, and, desc, sql, etc.
- Use db.select / insert / update / delete in Server Components and route
  handlers. Never write raw SQL inside TSX.
- Drizzle uses camelCase property names even when the underlying column is
  snake_case (row.userId, row.createdAt, row.passwordHash).
- UUID columns reject non-UUID strings. For placeholder user ids use
  '00000000-0000-0000-0000-000000000000', never 'guest'.
- Any page.tsx that reads from '@/lib/db' must export
  export const dynamic = "force-dynamic" so Next.js does not cache stale data.

## Schema Changes and Ad-Hoc Queries
Use the execute_sql tool for anything that is not a compiled Drizzle query:
- readonly=true for one-off SELECTs while exploring the data.
- readonly=false for CREATE TABLE, ALTER TABLE, DROP TABLE, and for seed
  INSERTs you need to run yourself.

After altering the schema with execute_sql, update lib/schema.ts so the
Drizzle types match the live tables. If the seed data references imagery,
generate the images with the generate_image tool first and store the
returned URLs in the INSERT statements.
`

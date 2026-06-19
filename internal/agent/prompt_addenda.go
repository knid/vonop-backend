package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
)

const SplitWebAddendum = `# SPLIT WEB ADDENDUM
This project uses the split web architecture.

## Source tree
- frontend/ is a Vite + React static frontend. Write browser UI only under frontend/.
- backend/ is optional and contains Next.js API routes only when the app needs server-side features.
- Do not write root Next.js app files such as app/page.tsx, app/layout.tsx, next.config.mjs, or root package.json.
- DESIGN.md is project metadata and is the only root-level file you may create or update.

## Frontend rules
- Use Vite/React imports under frontend/src/**.
- Call server features with fetch/apiFetch against relative /api/* URLs.
- Never put DATABASE_URL, WEMOB_AI_KEY, provider secrets, or server SDK calls in frontend/.

## Backend rules
- If the user asks for AI, database, auth, payments, uploads, webhooks, or private integrations, create backend/app/api/**/route.ts handlers.
- Backend route handlers may import backend/lib/db.ts or backend/lib/wemob-ai.ts after the matching setup tool creates them.
- The frontend talks to backend routes through /api/*; it never imports backend files.

## Backend Selection Rule

Do not treat backend as optional when the requested product includes any feature
that must work across devices, users, browser sessions, or deployments.

Create backend/app/api/** route handlers and persistent storage when the user asks for:
- editing, creating, deleting, or publishing content
- admin panels, dashboards, login, roles, or protected areas
- contact forms, lead capture, bookings, applications, submissions, or messages
- user accounts, authentication, profiles, saved data, or private data
- payments, uploads, AI, email sending, webhooks, integrations, or secrets
- "all functions work", "real app", "production", "I can manage it", or similar wording

Frontend-only localStorage is allowed only for demos, prototypes, mockups, or when
the user explicitly asks for a static/client-only app.

If the request says the owner/admin can manage content, default to:
- backend API routes
- persistent database/storage
- server-side validation
- frontend calling /api/*
- no hardcoded admin password in frontend`

const BackendOnlyAddendum = `# BACKEND/API-ONLY ADDENDUM
This project starts without a frontend. Build only the backend API unless the
user explicitly asks to add a web frontend.

## Source tree
- backend/ is the application root. Write Next.js API route handlers under
  backend/app/api/**/route.ts and server utilities under backend/lib/**.
- Do not write frontend/, root app/**, root package.json, or other root Next.js
  files while frontend_runtime is "none".
- DESIGN.md is project metadata and is the only root-level file you may create
  or update.

## Backend scope
- It is valid for the first generation to be a scaffold with backend/package.json
  and setup files only.
- Add routes, database, auth, AI, webhooks, uploads, cron-style endpoints, and
  integrations only when the user prompt calls for them.
- Keep secrets and provider SDKs server-side. Never expose DATABASE_URL,
  WEMOB_AI_KEY, auth secrets, or webhook signing secrets to client code.

## Adding a frontend later
- If the user asks for a web UI, first call enable_frontend. That changes the
  project to Vite React hosting, scaffolds frontend/, and unlocks frontend writes.
- After enable_frontend, build browser UI under frontend/ and call the existing
  backend through relative /api/* URLs. Never import backend files from frontend/.`

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
UPDATE the single ` + "`config`" + ` row — never INSERT. The execute_sql guard
blocks UPDATE without a WHERE, so target the row by its id even though
there is only one. Example:
    UPDATE config SET store_name = 'Acme', currency = 'EUR',
      tax_rate = 8,
      payment_config = '{"stripe": {"enabled": true}}'::jsonb
    WHERE id = (SELECT id FROM config LIMIT 1);
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
useCart, useStoreConfig, useCategories, useReviews, useWishlist; mutation
helpers addCartItem, applyDiscountCode, submitReview, etc.). All reads
happen in Server Components — client components receive catalog data
(products, single product) as props and read store-wide config +
categories from useStoreConfig/useCategories, which are seeded from
app/layout.tsx SSR with zero client round-trip. Every mutation POSTs to
a /api/* route; do not write Server Actions ("use server") — the template
is built around route handlers so the Origin/X-Forwarded-Host CSRF check
never trips under the preview proxy.

Do not import drizzle or '@/lib/db' in pages — route through the helpers.
Pages that already read cookies(), params, or searchParams are dynamic
automatically. For catalog pages that don't (product detail, category
pages), opt into a short revalidation window with
  export const revalidate = 60
so Caddy serves cached HTML instead of running a Neon round-trip per hit.

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

## Initial catalog size (HARD CAP)
The setup_store payload must contain exactly 2 categories and exactly 2
products (one per category). That's 4 catalog generate_image calls maximum —
2 category headers + 2 product shots. The merchant fills out the rest from
the dashboard. Do NOT pass 6+ products on first run — it is slow, expensive,
and the merchant is going to replace them anyway.

Hero, about, and other page imagery are separate from this cap — generate
them normally. The cap applies only to the setup_store payload.

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

Stripe / Iyzico / COD are wired in lib/payments/. For a provider to show up
in the checkout form: (1) the merchant connects it in the wemob dashboard
(keys injected at deploy), AND (2) config.payment_config in the tenant DB
has it enabled, e.g. '{"iyzico": {"enabled": true}}'. The dashboard
connect flow auto-enables iyzico/stripe in payment_config server-side now,
so you usually don't have to. But the AI must always make sure that the
config in setup_store reflects the user's intent, and the merchant's
connected providers.

## Turkish stores: hard defaults

If the user is building for Turkey (prompt is in Turkish, mentions TRY/TL/lira,
or names Turkish brands), set ALL of these in setup_store.config on the first
turn — do NOT rely on the seed defaults (USD/en):
  - currency: "TRY"
  - locale:   "tr"
  - tax_rate: 20      (Turkish KDV; merchant can change later)
  - payment_config: {"iyzico": {"enabled": true}, "cod": {"enabled": true}}

Iyzico installments only fire for TRY, the hosted form renders in Turkish only
when locale=tr, and a TR store with USD pricing is a launch-blocker. If you
detect Turkish but skip these, the merchant's customers will see a broken
checkout.
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
func buildAIAddendum(deps *RunDependencies, project *models.AppProject) string {
	text, _ := buildAIAddendumWithLangfuse(contextWithoutLangfuse(), depsWithoutLangfuse(deps), project)
	return text
}

func buildAIAddendumWithLangfuse(ctx context.Context, deps *RunDependencies, project *models.AppProject) (string, observability.PromptLink) {
	allowedChat := gatewayAllowedChatModels(deps)
	allowedImage := gatewayAllowedImageModels(deps)

	modelBlock := "Available chat models will be listed when you call\nsetup_wemob_ai_gateway — use whatever that tool reports."
	imageModelBlock := "Available image models will be listed when you call setup_wemob_ai_gateway."
	defaultModel := constants.GatewayDefaultChatModel
	if len(allowedChat) > 0 {
		modelBlock = "Available chat models on the user's current plan (pass as the\nmodel field on wemobAI().chat.completions.create — anything else\nwill 403 at runtime):\n" + renderGatewayChatModelsBlock(allowedChat)
		defaultModel = defaultGatewayChatModel(allowedChat)
	}
	if len(allowedImage) > 0 {
		imageModelBlock = "Available image models on the user's current plan (pass as the\nmodel field on generateImage — anything else will 403 at runtime):\n" + renderGatewayImageModelsBlock(allowedImage)
	}

	if project.UsesSplitWebRuntime() {
		fallback := buildAISplitWebAddendum(modelBlock, imageModelBlock, defaultModel)
		return resolveLangfusePrompt(ctx, deps, observability.PromptAgentAISplitWeb, fallback, map[string]string{
			"model_block":       modelBlock,
			"image_model_block": imageModelBlock,
			"default_model":     defaultModel,
		})
	}

	fallback := `# AI ADDENDUM
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
- generateImage({ model, prompt, imageSize, numImages, imageUrl, imageUrls }) returns string[] of
  public image URLs. Use it for AI-generated imagery the user requests at
  runtime (not for placeholder hero art — use the generate_image tool for that).

` + modelBlock + `

` + imageModelBlock + `

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
	return resolveLangfusePrompt(ctx, deps, observability.PromptAgentAINextJS, fallback, map[string]string{
		"model_block":       modelBlock,
		"image_model_block": imageModelBlock,
		"default_model":     defaultModel,
	})
}

func buildAISplitWebAddendum(modelBlock, imageModelBlock, defaultModel string) string {
	return `# AI ADDENDUM (split web + backend)
This project has the built-in Wemob AI Gateway wired through its backend.
In every user-facing message, call it "AI" or "yapay zeka" — never "Wemob
Gateway", "OpenRouter", "Claude", "GPT", "Anthropic", or any underlying model
name.

The AI helper lives at backend/lib/wemob-ai.ts. Import it only from backend
route handlers:

    import { wemobAI, generateImage } from '@/lib/wemob-ai';

` + modelBlock + `

` + imageModelBlock + `

The openai npm package is already in backend/package.json — do not add it
again, and do not add other AI SDKs.

## Server-Side Only
WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL are auto-injected into the
backend deployment. NEVER import backend/lib/wemob-ai.ts from frontend/.

## Pattern
- Backend route: backend/app/api/chat/route.ts calls
    const res = await wemobAI().chat.completions.create({
      model: '` + defaultModel + `',
      messages,
    });
  and returns JSON or a streamed Response.
- Frontend UI under frontend/ calls apiFetch('/api/chat', { method: 'POST', json: { messages } }).
- Image generation happens in backend routes through generateImage({ prompt });
  the frontend renders returned URLs with a normal img element.

Do not call setup_wemob_ai_gateway again — the helper already exists.
`
}

// AttachmentsAddendum builds the system-prompt section that lists every
// non-deleted asset in the project (capped at the controller layer). The
// LLM uses this to know what files exist, embed URLs in generated app code,
// and call read_attachment with stable IDs. Returns "" when the pool is
// empty so the prompt doesn't carry a useless header.
func AttachmentsAddendum(pool []models.AppAsset) string {
	text, _ := AttachmentsAddendumWithLangfuse(contextWithoutLangfuse(), nil, pool)
	return text
}

func AttachmentsAddendumWithLangfuse(ctx context.Context, deps *RunDependencies, pool []models.AppAsset) (string, observability.PromptLink) {
	if len(pool) == 0 {
		return "", observability.PromptLink{}
	}
	rows := attachmentRows(pool)
	fallback := "## Project Assets\n\n" +
		"You have access to the following files uploaded to this project. " +
		"Reference them by URL in any code you generate. " +
		"To read the contents of a non-image file, call the `read_attachment` tool with its ID. " +
		"Image attachments sent on the current message are already visible to you — no tool call needed.\n\n" +
		"| ID | Name | Type | Size | URL |\n" +
		"|----|------|------|------|-----|\n" +
		rows
	return resolveLangfusePrompt(ctx, deps, observability.PromptAgentAttachmentsAddendum, fallback, map[string]string{
		"asset_rows": strings.TrimRight(rows, "\n"),
	})
}

func attachmentRows(pool []models.AppAsset) string {
	var sb strings.Builder
	for _, a := range pool {
		// FileName, ContentType, and PublicURL originate from user uploads.
		// A filename like `|oops|\n# NEW SYSTEM INSTRUCTIONS\n ...` would
		// otherwise break the table row and inject text that the LLM reads
		// as part of the system prompt. Scrub newlines, pipes, and
		// backticks so the cell stays a cell.
		sb.WriteString("| ")
		sb.WriteString(a.ID.String())
		sb.WriteString(" | ")
		sb.WriteString(sanitizeAttachmentCell(a.FileName))
		sb.WriteString(" | ")
		sb.WriteString(sanitizeAttachmentCell(a.ContentType))
		sb.WriteString(" | ")
		sb.WriteString(humanReadableBytes(a.FileSize))
		sb.WriteString(" | ")
		sb.WriteString(sanitizeAttachmentCell(a.PublicURL))
		sb.WriteString(" |\n")
	}
	return sb.String()
}

// sanitizeAttachmentCell neutralises user-controlled strings before they are
// written as Markdown table cells in the system prompt. Drops control chars
// and newlines (would break the row), replaces `|` (would create new columns),
// and escapes backticks (would open code spans that let subsequent tokens
// render as model-targeted instructions). Long values are truncated to keep a
// single crafted filename from dominating the prompt budget.
func sanitizeAttachmentCell(s string) string {
	const maxLen = 200
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n', r == '\r', r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// drop other control chars silently
		case r == '|':
			b.WriteString(`\|`)
		case r == '`':
			b.WriteByte('\'')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > maxLen {
		out = out[:maxLen] + "…"
	}
	return out
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

// DatabaseAddendumNative is appended to the base prompt for mobile_app_native
// projects whose backend/ Postgres database has been provisioned (signaled by
// project.Settings["has_database"] = true). Replaces DatabaseAddendum on
// native projects — the file paths and import patterns differ.
//
// Independent of LocalDatabaseAddendum — a synced app may carry both.
const DatabaseAddendumNative = `# DATABASE ADDENDUM (native + backend)

This project has a wemob db wired through its Next.js backend. Schema files:
- backend/lib/db.ts — Drizzle Postgres client. Imports the schema.
- backend/lib/schema.ts — Drizzle pgTable definitions; you author this.
- backend/app/api/<resource>/route.ts — CRUD endpoints; you author these.
- mobile/lib/api.ts — already in the Expo template; mobile screens call backend routes through apiFetch.

Pattern (must follow exactly):
1. backend/lib/db.ts is imported ONLY from server-side code under backend/.
   Examples: backend/app/api/<x>/route.ts, server actions, Server Components.
2. Mobile screens (mobile/**/*.tsx) call backend routes through apiFetch:
     const rows = await apiFetch<Item[]>('/api/<resource>');
3. NEVER import '@/lib/db' from anywhere under mobile/. NEVER reference
   DATABASE_URL in mobile code. NEVER expose connection strings through any
   API response.

Schema changes:
- ALTER TABLE / extra INSERTs go through the execute_sql tool. Use
  readonly=false for mutations, readonly=true for exploration.
- Keep backend/lib/schema.ts in sync with the live database — Drizzle's TS
  types come from it.

User-facing language:
- Call this "your database" / "wemob db" / "veritabanın". NEVER say Neon,
  Postgres, Drizzle, Supabase, or Firebase to the user.
`

const DatabaseAddendumSplitWeb = `# DATABASE ADDENDUM (split web + backend)

This project has a wemob db wired through its Next.js backend. Schema files:
- backend/lib/db.ts — Drizzle Postgres client. Imports the schema.
- backend/lib/schema.ts — Drizzle pgTable definitions; you author this.
- backend/app/api/<resource>/route.ts — CRUD endpoints; you author these.
- frontend/src/lib/api.ts — Vite frontend helper; frontend screens call backend routes through apiFetch.

Pattern:
1. backend/lib/db.ts is imported ONLY from server-side code under backend/.
2. Frontend components under frontend/ call backend routes through apiFetch:
     const rows = await apiFetch<Item[]>('/api/<resource>');
3. NEVER import backend files from frontend/. NEVER reference DATABASE_URL in frontend code.

Schema changes:
- ALTER TABLE / extra INSERTs go through execute_sql. Use readonly=false for mutations, readonly=true for exploration.
- Keep backend/lib/schema.ts in sync with the live database.

User-facing language:
- Call this "your database" / "wemob db" / "veritabanın". NEVER say Neon,
  Postgres, Drizzle, Supabase, or Firebase to the user.
`

// LocalDatabaseAddendum is appended to the base prompt for mobile_app_native
// projects whose on-device SQLite database has been provisioned (signaled by
// project.Settings["has_local_database"] = true). Independent of
// DatabaseAddendumNative — a synced app gets both.
const LocalDatabaseAddendum = `# LOCAL DATABASE ADDENDUM (on-device SQLite)

This native project has on-device SQLite wired via expo-sqlite + Drizzle.
Schema files:
- mobile/lib/db.tsx — Drizzle expo-sqlite client + DatabaseProvider component.
- mobile/lib/schema.ts — sqliteTable definitions.
- mobile/lib/migrations.ts — runSync(db) with idempotent CREATE TABLE
  IF NOT EXISTS / ALTER TABLE … ADD COLUMN. Runs on every app launch.

Pattern (must follow exactly):
1. Reactive reads in screens use useLiveQuery — never useEffect + setState
   to mirror db rows:
     import { useLiveQuery } from "drizzle-orm/expo-sqlite";
     const { data } = useLiveQuery(db.select().from(<table>));
2. Writes from event handlers use db.insert/update/delete — never inside
   render. Wrap multi-step writes in db.transaction() if you need atomicity.
3. NEVER import 'mobile/lib/db.tsx' from anything under backend/. The
   SQLite file is device-only.
4. NEVER persist auth tokens / secrets here — use expo-secure-store
   (already in the Expo template).

Schema changes:
- Edit mobile/lib/schema.ts AND mobile/lib/migrations.ts together. Append
  fresh CREATE TABLE IF NOT EXISTS / ALTER TABLE … ADD COLUMN statements;
  the migrations function is idempotent so re-running it is safe.
- Do not call provision_local_database again — it's one-shot.

User-facing language:
- Call this "your data" / "this app's storage" / "veritabanın". NEVER
  say SQLite, expo-sqlite, or Drizzle to the user.
`

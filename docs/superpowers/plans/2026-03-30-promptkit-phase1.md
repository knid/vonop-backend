# PromptKit Phase 1: Visual Composer + SDK

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the MVP — a visual prompt composition editor with n8n-style IF blocks and a TypeScript SDK that assembles prompts locally from synced configs.

**Architecture:** Next.js 16 full-stack app. React Flow for the composition canvas, Monaco for block editing, Drizzle ORM with Neon Postgres. TypeScript SDK connects via REST + SSE for LaunchDarkly-style config sync. Clerk handles auth with team workspaces.

**Tech Stack:** Next.js 16, TypeScript, Tailwind CSS v4, shadcn/ui, @xyflow/react, @monaco-editor/react, Drizzle ORM, Neon Postgres, Clerk, SSE

**Spec:** `docs/superpowers/specs/2026-03-30-promptkit-design.md`

**Repo:** New standalone project (not inside wemob-backend).

---

## File Structure

```
promptkit/
├── app/
│   ├── layout.tsx                    # Root layout (Clerk, fonts, theme)
│   ├── page.tsx                      # Dashboard
│   ├── globals.css                   # Tailwind v4 theme
│   ├── sign-in/[[...sign-in]]/page.tsx
│   ├── sign-up/[[...sign-up]]/page.tsx
│   ├── compositions/
│   │   ├── page.tsx                  # Composition list
│   │   └── [id]/
│   │       └── page.tsx              # Composition editor (React Flow)
│   ├── blocks/
│   │   └── page.tsx                  # Block library
│   ├── settings/
│   │   └── page.tsx                  # API keys, team, environments
│   └── api/
│       ├── blocks/
│       │   ├── route.ts              # GET (list), POST (create)
│       │   └── [id]/route.ts         # GET, PUT, DELETE
│       ├── compositions/
│       │   ├── route.ts              # GET (list), POST (create)
│       │   └── [id]/
│       │       ├── route.ts          # GET, PUT, DELETE
│       │       └── promote/route.ts  # POST (promote to env)
│       ├── sdk/
│       │   ├── config/[env]/route.ts # GET full config for SDK
│       │   └── stream/[env]/route.ts # SSE stream for live updates
│       └── api-keys/
│           └── route.ts              # GET, POST, DELETE
├── components/
│   ├── layout/
│   │   ├── sidebar.tsx               # App sidebar navigation
│   │   ├── header.tsx                # Page header with env switcher
│   │   └── command-palette.tsx       # Cmd+K palette
│   ├── editor/
│   │   ├── flow-canvas.tsx           # React Flow wrapper
│   │   ├── nodes/
│   │   │   ├── block-node.tsx        # Block node component
│   │   │   ├── if-boolean-node.tsx   # IF Boolean node
│   │   │   ├── if-switch-node.tsx    # IF Switch node
│   │   │   ├── merge-node.tsx        # Merge node
│   │   │   ├── start-node.tsx        # Start node
│   │   │   └── output-node.tsx       # Output node
│   │   ├── block-editor-panel.tsx    # Monaco panel (right side)
│   │   ├── preview-panel.tsx         # Live preview (bottom)
│   │   ├── context-schema-editor.tsx # Context schema config
│   │   └── node-palette.tsx          # Drag-to-add node palette
│   ├── blocks/
│   │   ├── block-list.tsx            # Searchable block list
│   │   └── block-card.tsx            # Block card component
│   └── dashboard/
│       ├── stat-card.tsx             # Stat card component
│       ├── composition-grid.tsx      # Composition quick-access grid
│       └── recent-changes.tsx        # Activity feed
├── lib/
│   ├── db.ts                         # Drizzle client
│   ├── schema.ts                     # Drizzle schema (all tables)
│   ├── utils.ts                      # cn() utility
│   └── graph-engine.ts              # Flow graph → assembled text
├── drizzle.config.ts                 # Drizzle Kit config
├── package.json
├── tsconfig.json
├── next.config.ts
└── .env.local

sdk/                                   # Separate package: @promptkit/sdk
├── src/
│   ├── index.ts                      # Main export
│   ├── client.ts                     # PromptKit class
│   ├── compose.ts                    # Local assembly engine
│   ├── sync.ts                       # Config fetcher + SSE listener
│   ├── types.ts                      # All TypeScript types
│   └── hash.ts                       # Deterministic variant hashing
├── package.json
├── tsconfig.json
└── vitest.config.ts
```

---

## Task 1: Project Scaffolding

**Files:**
- Create: `promptkit/` (entire project scaffold)
- Create: `promptkit/package.json`, `promptkit/next.config.ts`, `promptkit/tsconfig.json`
- Create: `promptkit/app/layout.tsx`, `promptkit/app/globals.css`, `promptkit/app/page.tsx`
- Create: `promptkit/lib/utils.ts`

- [ ] **Step 1: Scaffold Next.js project**

```bash
npx create-next-app@latest promptkit \
  --typescript --tailwind --app --src-dir=false \
  --import-alias "@/*" --turbopack
cd promptkit
```

- [ ] **Step 2: Install core dependencies**

```bash
npm install @clerk/nextjs drizzle-orm @neondatabase/serverless \
  @xyflow/react @monaco-editor/react \
  lucide-react clsx tailwind-merge cmdk sonner \
  geist
npm install -D drizzle-kit
```

- [ ] **Step 3: Initialize shadcn/ui**

```bash
npx shadcn@latest init
npx shadcn@latest add button card input badge skeleton \
  dialog dropdown-menu tabs tooltip separator \
  scroll-area select switch popover command
```

- [ ] **Step 4: Create globals.css with dark zinc theme**

```css
/* app/globals.css */
@import "tailwindcss";
@import "@radix-ui/themes/styles.css";

@theme {
  --color-background: #09090b;
  --color-foreground: #fafafa;
  --color-card: #18181b;
  --color-card-foreground: #fafafa;
  --color-popover: #18181b;
  --color-popover-foreground: #fafafa;
  --color-primary: #7c3aed;
  --color-primary-foreground: #fafafa;
  --color-secondary: #27272a;
  --color-secondary-foreground: #fafafa;
  --color-muted: #27272a;
  --color-muted-foreground: #a1a1aa;
  --color-accent: #27272a;
  --color-accent-foreground: #fafafa;
  --color-destructive: #ef4444;
  --color-destructive-foreground: #fafafa;
  --color-border: #27272a;
  --color-input: #27272a;
  --color-ring: #7c3aed;
  --color-success: #4ade80;
  --color-warning: #f59e0b;
  --radius: 0.5rem;
}

body {
  font-family: "Geist Sans", system-ui, -apple-system, sans-serif;
  background: var(--color-background);
  color: var(--color-foreground);
}

code, pre, .font-mono {
  font-family: "Geist Mono", monospace;
}
```

- [ ] **Step 5: Create root layout with Clerk + Geist fonts**

```tsx
// app/layout.tsx
import "./globals.css"
import { ClerkProvider } from "@clerk/nextjs"
import { dark } from "@clerk/themes"
import { GeistSans } from "geist/font/sans"
import { GeistMono } from "geist/font/mono"
import { Toaster } from "sonner"
import type { Metadata } from "next"

export const metadata: Metadata = {
  title: "PromptKit",
  description: "The prompt compiler for AI-first teams",
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <ClerkProvider appearance={{ baseTheme: dark }}>
      <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable}`}>
        <body className="antialiased">
          {children}
          <Toaster theme="dark" />
        </body>
      </html>
    </ClerkProvider>
  )
}
```

- [ ] **Step 6: Create placeholder dashboard page**

```tsx
// app/page.tsx
export default function DashboardPage() {
  return (
    <div className="flex min-h-dvh items-center justify-center">
      <h1 className="text-2xl font-semibold tracking-tight">PromptKit</h1>
    </div>
  )
}
```

- [ ] **Step 7: Create .env.local template**

```bash
# .env.local
NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_...
CLERK_SECRET_KEY=sk_test_...
NEXT_PUBLIC_CLERK_SIGN_IN_URL=/sign-in
NEXT_PUBLIC_CLERK_SIGN_UP_URL=/sign-up
DATABASE_URL=postgresql://...
```

- [ ] **Step 8: Verify dev server starts**

```bash
npm run dev
# Expected: Next.js 16 dev server on http://localhost:3000
# Visit http://localhost:3000 — see "PromptKit" centered on dark background
```

- [ ] **Step 9: Commit**

```bash
git init && git add -A
git commit -m "feat: scaffold PromptKit with Next.js 16, Clerk, shadcn/ui, dark theme"
```

---

## Task 2: Database Schema

**Files:**
- Create: `promptkit/lib/db.ts`
- Create: `promptkit/lib/schema.ts`
- Create: `promptkit/drizzle.config.ts`

- [ ] **Step 1: Create Drizzle client**

```typescript
// lib/db.ts
import { neon } from "@neondatabase/serverless"
import { drizzle } from "drizzle-orm/neon-http"
import * as schema from "./schema"

const sql = neon(process.env.DATABASE_URL!)
export const db = drizzle(sql, { schema })
```

- [ ] **Step 2: Create full schema**

```typescript
// lib/schema.ts
import {
  pgTable, text, uuid, timestamp, integer, boolean, jsonb, pgEnum
} from "drizzle-orm/pg-core"

export const envEnum = pgEnum("environment", ["dev", "staging", "prod"])

// ─── Teams (Clerk-synced) ───
export const teams = pgTable("teams", {
  id: uuid("id").primaryKey().defaultRandom(),
  clerkOrgId: text("clerk_org_id").notNull().unique(),
  name: text("name").notNull(),
  createdAt: timestamp("created_at").notNull().defaultNow(),
})

// ─── API Keys ───
export const apiKeys = pgTable("api_keys", {
  id: uuid("id").primaryKey().defaultRandom(),
  teamId: uuid("team_id").notNull().references(() => teams.id),
  name: text("name").notNull(),
  keyHash: text("key_hash").notNull(),
  keyPrefix: text("key_prefix").notNull(), // "pk_live_abc..."
  environment: envEnum("environment").notNull().default("dev"),
  lastUsedAt: timestamp("last_used_at"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
})

// ─── Blocks ───
export const blocks = pgTable("blocks", {
  id: uuid("id").primaryKey().defaultRandom(),
  teamId: uuid("team_id").notNull().references(() => teams.id),
  name: text("name").notNull(),
  description: text("description"),
  content: text("content").notNull().default(""),
  version: integer("version").notNull().default(1),
  tags: jsonb("tags").notNull().default([]),
  createdAt: timestamp("created_at").notNull().defaultNow(),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
})

export const blockVersions = pgTable("block_versions", {
  id: uuid("id").primaryKey().defaultRandom(),
  blockId: uuid("block_id").notNull().references(() => blocks.id),
  version: integer("version").notNull(),
  content: text("content").notNull(),
  createdAt: timestamp("created_at").notNull().defaultNow(),
  createdBy: text("created_by"), // Clerk userId
})

// ─── Compositions ───
export const compositions = pgTable("compositions", {
  id: uuid("id").primaryKey().defaultRandom(),
  teamId: uuid("team_id").notNull().references(() => teams.id),
  name: text("name").notNull(),
  description: text("description"),
  // React Flow graph stored as JSON
  graph: jsonb("graph").notNull().default({ nodes: [], edges: [] }),
  // Context schema: user-defined params for IF nodes
  contextSchema: jsonb("context_schema").notNull().default([]),
  version: integer("version").notNull().default(1),
  createdAt: timestamp("created_at").notNull().defaultNow(),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
})

export const compositionVersions = pgTable("composition_versions", {
  id: uuid("id").primaryKey().defaultRandom(),
  compositionId: uuid("composition_id").notNull().references(() => compositions.id),
  version: integer("version").notNull(),
  graph: jsonb("graph").notNull(),
  contextSchema: jsonb("context_schema").notNull(),
  createdAt: timestamp("created_at").notNull().defaultNow(),
  createdBy: text("created_by"),
})

// ─── Deployments (which version is live per env) ───
export const deployments = pgTable("deployments", {
  id: uuid("id").primaryKey().defaultRandom(),
  compositionId: uuid("composition_id").notNull().references(() => compositions.id),
  environment: envEnum("environment").notNull(),
  version: integer("version").notNull(),
  deployedAt: timestamp("deployed_at").notNull().defaultNow(),
  deployedBy: text("deployed_by"),
})

// ─── Assembly Logs ───
export const assemblyLogs = pgTable("assembly_logs", {
  id: uuid("id").primaryKey().defaultRandom(),
  teamId: uuid("team_id").notNull().references(() => teams.id),
  compositionId: uuid("composition_id").notNull(),
  compositionVersion: integer("composition_version").notNull(),
  environment: envEnum("environment").notNull(),
  context: jsonb("context"), // what context was passed
  resolvedBlocks: jsonb("resolved_blocks"), // which blocks were included
  variantId: text("variant_id"), // which A/B variant was picked
  tokenCount: integer("token_count"),
  assembledAt: timestamp("assembled_at").notNull().defaultNow(),
})
```

- [ ] **Step 3: Create Drizzle config**

```typescript
// drizzle.config.ts
import { defineConfig } from "drizzle-kit"

export default defineConfig({
  schema: "./lib/schema.ts",
  out: "./drizzle",
  dialect: "postgresql",
  dbCredentials: {
    url: process.env.DATABASE_URL!,
  },
})
```

- [ ] **Step 4: Generate and push migration**

```bash
npx drizzle-kit generate
npx drizzle-kit push
# Expected: Tables created in Neon database
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: database schema — blocks, compositions, versions, deployments, assembly logs"
```

---

## Task 3: API Routes — Blocks CRUD

**Files:**
- Create: `promptkit/app/api/blocks/route.ts`
- Create: `promptkit/app/api/blocks/[id]/route.ts`

- [ ] **Step 1: Create blocks list + create route**

```typescript
// app/api/blocks/route.ts
import { db } from "@/lib/db"
import { blocks, blockVersions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, desc } from "drizzle-orm"
import { NextResponse } from "next/server"

export async function GET() {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const teamBlocks = await db
    .select()
    .from(blocks)
    .where(eq(blocks.teamId, orgId))
    .orderBy(desc(blocks.updatedAt))

  return NextResponse.json(teamBlocks)
}

export async function POST(req: Request) {
  const { orgId, userId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const body = await req.json()
  const { name, description, content, tags } = body

  const [block] = await db.insert(blocks).values({
    teamId: orgId,
    name,
    description: description ?? "",
    content: content ?? "",
    tags: tags ?? [],
  }).returning()

  // Create initial version
  await db.insert(blockVersions).values({
    blockId: block.id,
    version: 1,
    content: block.content,
    createdBy: userId,
  })

  return NextResponse.json(block, { status: 201 })
}
```

- [ ] **Step 2: Create block get/update/delete route**

```typescript
// app/api/blocks/[id]/route.ts
import { db } from "@/lib/db"
import { blocks, blockVersions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, and } from "drizzle-orm"
import { NextResponse } from "next/server"

export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  const [block] = await db
    .select()
    .from(blocks)
    .where(and(eq(blocks.id, id), eq(blocks.teamId, orgId)))

  if (!block) return NextResponse.json({ error: "Not found" }, { status: 404 })
  return NextResponse.json(block)
}

export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId, userId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  const body = await req.json()
  const { name, description, content, tags } = body

  const [existing] = await db
    .select()
    .from(blocks)
    .where(and(eq(blocks.id, id), eq(blocks.teamId, orgId)))

  if (!existing) return NextResponse.json({ error: "Not found" }, { status: 404 })

  const newVersion = existing.version + 1

  const [updated] = await db
    .update(blocks)
    .set({
      name: name ?? existing.name,
      description: description ?? existing.description,
      content: content ?? existing.content,
      tags: tags ?? existing.tags,
      version: content !== undefined ? newVersion : existing.version,
      updatedAt: new Date(),
    })
    .where(eq(blocks.id, id))
    .returning()

  // Create version snapshot if content changed
  if (content !== undefined && content !== existing.content) {
    await db.insert(blockVersions).values({
      blockId: id,
      version: newVersion,
      content,
      createdBy: userId,
    })
  }

  return NextResponse.json(updated)
}

export async function DELETE(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  await db.delete(blocks).where(and(eq(blocks.id, id), eq(blocks.teamId, orgId)))
  return NextResponse.json({ ok: true })
}
```

- [ ] **Step 3: Verify with curl**

```bash
# Start dev server, then test:
# Create a block
curl -X POST http://localhost:3000/api/blocks \
  -H "Content-Type: application/json" \
  -d '{"name":"role","content":"You are a senior engineer...","tags":["core"]}'
# Expected: 201 with block JSON including id, version: 1
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: blocks CRUD API with version history"
```

---

## Task 4: API Routes — Compositions CRUD

**Files:**
- Create: `promptkit/app/api/compositions/route.ts`
- Create: `promptkit/app/api/compositions/[id]/route.ts`
- Create: `promptkit/app/api/compositions/[id]/promote/route.ts`

- [ ] **Step 1: Create compositions list + create route**

```typescript
// app/api/compositions/route.ts
import { db } from "@/lib/db"
import { compositions, compositionVersions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, desc } from "drizzle-orm"
import { NextResponse } from "next/server"

export async function GET() {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const result = await db
    .select()
    .from(compositions)
    .where(eq(compositions.teamId, orgId))
    .orderBy(desc(compositions.updatedAt))

  return NextResponse.json(result)
}

export async function POST(req: Request) {
  const { orgId, userId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const body = await req.json()
  const { name, description } = body

  // Default graph: Start → Output
  const defaultGraph = {
    nodes: [
      { id: "start", type: "start", position: { x: 50, y: 200 }, data: {} },
      { id: "output", type: "output", position: { x: 600, y: 200 }, data: {} },
    ],
    edges: [
      { id: "start-output", source: "start", target: "output" },
    ],
  }

  const [comp] = await db.insert(compositions).values({
    teamId: orgId,
    name,
    description: description ?? "",
    graph: defaultGraph,
    contextSchema: [],
  }).returning()

  await db.insert(compositionVersions).values({
    compositionId: comp.id,
    version: 1,
    graph: defaultGraph,
    contextSchema: [],
    createdBy: userId,
  })

  return NextResponse.json(comp, { status: 201 })
}
```

- [ ] **Step 2: Create composition get/update/delete route**

```typescript
// app/api/compositions/[id]/route.ts
import { db } from "@/lib/db"
import { compositions, compositionVersions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, and } from "drizzle-orm"
import { NextResponse } from "next/server"

export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  const [comp] = await db
    .select()
    .from(compositions)
    .where(and(eq(compositions.id, id), eq(compositions.teamId, orgId)))

  if (!comp) return NextResponse.json({ error: "Not found" }, { status: 404 })
  return NextResponse.json(comp)
}

export async function PUT(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId, userId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  const body = await req.json()
  const { name, description, graph, contextSchema } = body

  const [existing] = await db
    .select()
    .from(compositions)
    .where(and(eq(compositions.id, id), eq(compositions.teamId, orgId)))

  if (!existing) return NextResponse.json({ error: "Not found" }, { status: 404 })

  const graphChanged = graph !== undefined
  const newVersion = graphChanged ? existing.version + 1 : existing.version

  const [updated] = await db
    .update(compositions)
    .set({
      name: name ?? existing.name,
      description: description ?? existing.description,
      graph: graph ?? existing.graph,
      contextSchema: contextSchema ?? existing.contextSchema,
      version: newVersion,
      updatedAt: new Date(),
    })
    .where(eq(compositions.id, id))
    .returning()

  if (graphChanged) {
    await db.insert(compositionVersions).values({
      compositionId: id,
      version: newVersion,
      graph: updated.graph as object,
      contextSchema: updated.contextSchema as object,
      createdBy: userId,
    })
  }

  return NextResponse.json(updated)
}

export async function DELETE(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  await db.delete(compositions).where(and(eq(compositions.id, id), eq(compositions.teamId, orgId)))
  return NextResponse.json({ ok: true })
}
```

- [ ] **Step 3: Create promote route**

```typescript
// app/api/compositions/[id]/promote/route.ts
import { db } from "@/lib/db"
import { compositions, deployments } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, and } from "drizzle-orm"
import { NextResponse } from "next/server"

export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { orgId, userId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })
  const { id } = await params

  const body = await req.json()
  const { environment } = body // "dev" | "staging" | "prod"

  const [comp] = await db
    .select()
    .from(compositions)
    .where(and(eq(compositions.id, id), eq(compositions.teamId, orgId)))

  if (!comp) return NextResponse.json({ error: "Not found" }, { status: 404 })

  const [deployment] = await db.insert(deployments).values({
    compositionId: id,
    environment,
    version: comp.version,
    deployedBy: userId,
  }).returning()

  return NextResponse.json(deployment, { status: 201 })
}
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: compositions CRUD API with versioning and promote-to-env"
```

---

## Task 5: App Shell — Sidebar + Layout

**Files:**
- Create: `promptkit/components/layout/sidebar.tsx`
- Create: `promptkit/components/layout/header.tsx`
- Modify: `promptkit/app/layout.tsx`
- Create: `promptkit/app/(app)/layout.tsx` (authenticated layout)

- [ ] **Step 1: Create sidebar component**

```tsx
// components/layout/sidebar.tsx
"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  LayoutDashboard, Puzzle, Boxes, GitBranch,
  Settings, Key
} from "lucide-react"
import { cn } from "@/lib/utils"

const navItems = [
  { href: "/", label: "Dashboard", icon: LayoutDashboard },
  { href: "/compositions", label: "Compositions", icon: GitBranch },
  { href: "/blocks", label: "Blocks", icon: Boxes },
]

const bottomItems = [
  { href: "/settings", label: "Settings", icon: Settings },
]

export function Sidebar() {
  const pathname = usePathname()

  return (
    <aside className="flex h-dvh w-[200px] flex-col border-r border-border bg-background p-2">
      {/* Logo */}
      <div className="flex items-center gap-2 px-2 py-1 mb-4">
        <div className="flex h-6 w-6 items-center justify-center rounded-md bg-gradient-to-br from-primary to-blue-500">
          <span className="text-[10px] font-extrabold text-white">P</span>
        </div>
        <span className="text-sm font-semibold tracking-tight">PromptKit</span>
      </div>

      {/* Nav */}
      <nav className="flex flex-1 flex-col gap-0.5">
        {navItems.map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
              pathname === item.href
                ? "bg-secondary text-foreground font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="h-4 w-4 opacity-50" />
            {item.label}
          </Link>
        ))}
      </nav>

      {/* Bottom */}
      <div className="flex flex-col gap-0.5 border-t border-border pt-2">
        {bottomItems.map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
              pathname === item.href
                ? "bg-secondary text-foreground font-medium"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <item.icon className="h-4 w-4 opacity-50" />
            {item.label}
          </Link>
        ))}
      </div>
    </aside>
  )
}
```

- [ ] **Step 2: Create authenticated layout with sidebar**

```tsx
// app/(app)/layout.tsx
import { auth } from "@clerk/nextjs/server"
import { redirect } from "next/navigation"
import { Sidebar } from "@/components/layout/sidebar"

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const { userId } = await auth()
  if (!userId) redirect("/sign-in")

  return (
    <div className="flex min-h-dvh">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-6">{children}</main>
    </div>
  )
}
```

- [ ] **Step 3: Move dashboard page into (app) group**

Move `app/page.tsx` to `app/(app)/page.tsx`. Create sign-in/sign-up pages:

```tsx
// app/sign-in/[[...sign-in]]/page.tsx
import { SignIn } from "@clerk/nextjs"

export default function SignInPage() {
  return (
    <div className="flex min-h-dvh items-center justify-center">
      <SignIn />
    </div>
  )
}
```

```tsx
// app/sign-up/[[...sign-up]]/page.tsx
import { SignUp } from "@clerk/nextjs"

export default function SignUpPage() {
  return (
    <div className="flex min-h-dvh items-center justify-center">
      <SignUp />
    </div>
  )
}
```

- [ ] **Step 4: Verify sidebar renders with navigation**

```bash
npm run dev
# Visit http://localhost:3000
# Expected: Dark sidebar with PromptKit logo, Dashboard/Compositions/Blocks nav, Settings at bottom
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: app shell with sidebar navigation, Clerk auth, authenticated layout"
```

---

## Task 6: Block Library Page

**Files:**
- Create: `promptkit/app/(app)/blocks/page.tsx`
- Create: `promptkit/components/blocks/block-list.tsx`
- Create: `promptkit/components/blocks/block-card.tsx`

- [ ] **Step 1: Create block card component**

```tsx
// components/blocks/block-card.tsx
"use client"

import { Boxes, Hash } from "lucide-react"
import { Badge } from "@/components/ui/badge"

interface BlockCardProps {
  block: {
    id: string
    name: string
    description: string | null
    content: string
    version: number
    tags: string[]
    updatedAt: string
  }
  onClick: () => void
}

export function BlockCard({ block, onClick }: BlockCardProps) {
  const tokenEstimate = Math.round(block.content.length / 4)

  return (
    <button
      onClick={onClick}
      className="w-full rounded-lg border border-border bg-card p-3 text-left transition-colors hover:border-primary/30"
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Boxes className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="text-sm font-medium">{block.name}</span>
        </div>
        <span className="font-mono text-xs text-muted-foreground">v{block.version}</span>
      </div>
      {block.description && (
        <p className="mt-1.5 text-xs text-muted-foreground line-clamp-1">{block.description}</p>
      )}
      <div className="mt-2 flex items-center gap-2">
        <span className="font-mono text-[10px] text-muted-foreground">{tokenEstimate} tokens</span>
        {(block.tags as string[]).map((tag) => (
          <Badge key={tag} variant="secondary" className="text-[10px] px-1.5 py-0">
            {tag}
          </Badge>
        ))}
      </div>
    </button>
  )
}
```

- [ ] **Step 2: Create block list with search + create dialog**

```tsx
// components/blocks/block-list.tsx
"use client"

import { useState } from "react"
import { Plus, Search } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { BlockCard } from "./block-card"
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger
} from "@/components/ui/dialog"

interface Block {
  id: string
  name: string
  description: string | null
  content: string
  version: number
  tags: string[]
  updatedAt: string
}

export function BlockList({ initialBlocks }: { initialBlocks: Block[] }) {
  const [blocks, setBlocks] = useState(initialBlocks)
  const [search, setSearch] = useState("")
  const [newName, setNewName] = useState("")
  const [dialogOpen, setDialogOpen] = useState(false)

  const filtered = blocks.filter((b) =>
    b.name.toLowerCase().includes(search.toLowerCase())
  )

  async function createBlock() {
    const res = await fetch("/api/blocks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: newName, content: "" }),
    })
    const block = await res.json()
    setBlocks([block, ...blocks])
    setNewName("")
    setDialogOpen(false)
  }

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search blocks..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9"
          />
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button size="sm" className="gap-1.5">
              <Plus className="h-3.5 w-3.5" /> New Block
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Block</DialogTitle>
            </DialogHeader>
            <Input
              placeholder="Block name (e.g. role, design-philosophy)"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && createBlock()}
            />
            <Button onClick={createBlock} disabled={!newName.trim()}>Create</Button>
          </DialogContent>
        </Dialog>
      </div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {filtered.map((block) => (
          <BlockCard key={block.id} block={block} onClick={() => {}} />
        ))}
        {filtered.length === 0 && (
          <p className="col-span-full py-12 text-center text-sm text-muted-foreground">
            {search ? "No blocks match your search." : "No blocks yet. Create your first one."}
          </p>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Create blocks page (server component)**

```tsx
// app/(app)/blocks/page.tsx
import { db } from "@/lib/db"
import { blocks } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, desc } from "drizzle-orm"
import { redirect } from "next/navigation"
import { BlockList } from "@/components/blocks/block-list"

export const dynamic = "force-dynamic"

export default async function BlocksPage() {
  const { orgId } = await auth()
  if (!orgId) redirect("/sign-in")

  const teamBlocks = await db
    .select()
    .from(blocks)
    .where(eq(blocks.teamId, orgId))
    .orderBy(desc(blocks.updatedAt))

  return (
    <div>
      <h1 className="text-lg font-semibold tracking-tight mb-4">Blocks</h1>
      <BlockList initialBlocks={JSON.parse(JSON.stringify(teamBlocks))} />
    </div>
  )
}
```

- [ ] **Step 4: Verify blocks page**

```bash
npm run dev
# Visit http://localhost:3000/blocks
# Expected: Search bar, "New Block" button, empty state message
# Click New Block → create "role" → see it appear in the grid
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: block library page with search, create, and card grid"
```

---

## Task 7: Composition Editor — React Flow Canvas

**Files:**
- Create: `promptkit/components/editor/flow-canvas.tsx`
- Create: `promptkit/components/editor/nodes/start-node.tsx`
- Create: `promptkit/components/editor/nodes/output-node.tsx`
- Create: `promptkit/components/editor/nodes/block-node.tsx`
- Create: `promptkit/components/editor/node-palette.tsx`
- Create: `promptkit/app/(app)/compositions/[id]/page.tsx`

- [ ] **Step 1: Create Start node**

```tsx
// components/editor/nodes/start-node.tsx
"use client"

import { Handle, Position } from "@xyflow/react"

export function StartNode() {
  return (
    <div className="rounded-lg border border-border bg-card px-3.5 py-2">
      <div className="flex items-center gap-1.5">
        <div className="h-2 w-2 rounded-full bg-success" />
        <span className="text-xs font-semibold">Start</span>
      </div>
      <p className="mt-0.5 text-[10px] text-muted-foreground">Context input</p>
      <Handle type="source" position={Position.Right} className="!bg-success !h-2 !w-2" />
    </div>
  )
}
```

- [ ] **Step 2: Create Output node**

```tsx
// components/editor/nodes/output-node.tsx
"use client"

import { Handle, Position } from "@xyflow/react"

export function OutputNode() {
  return (
    <div className="rounded-lg border border-border bg-card px-3.5 py-2">
      <Handle type="target" position={Position.Left} className="!bg-destructive !h-2 !w-2" />
      <div className="flex items-center gap-1.5">
        <div className="h-2 w-2 rounded-full bg-destructive" />
        <span className="text-xs font-semibold">Output</span>
      </div>
      <p className="mt-0.5 text-[10px] text-muted-foreground">Assembled prompt</p>
    </div>
  )
}
```

- [ ] **Step 3: Create Block node**

```tsx
// components/editor/nodes/block-node.tsx
"use client"

import { Handle, Position, type NodeProps } from "@xyflow/react"

interface BlockNodeData {
  label: string
  blockId: string
  tokenCount?: number
  description?: string
}

export function BlockNode({ data }: NodeProps) {
  const { label, tokenCount, description } = data as BlockNodeData

  return (
    <div className="rounded-lg border border-green-900/50 bg-green-950/30 px-3 py-2 min-w-[130px]">
      <Handle type="target" position={Position.Left} className="!bg-success !h-2 !w-2" />
      <div className="text-[9px] font-semibold text-success mb-0.5">BLOCK</div>
      <div className="text-xs font-medium text-foreground">{label}</div>
      {description && (
        <div className="text-[9px] text-muted-foreground mt-0.5 line-clamp-1">{description}</div>
      )}
      {tokenCount !== undefined && (
        <div className="text-[9px] font-mono text-muted-foreground mt-1">{tokenCount} tokens</div>
      )}
      <Handle type="source" position={Position.Right} className="!bg-success !h-2 !w-2" />
    </div>
  )
}
```

- [ ] **Step 4: Create Flow Canvas wrapper**

```tsx
// components/editor/flow-canvas.tsx
"use client"

import { useCallback, useMemo } from "react"
import {
  ReactFlow, Background, Controls, MiniMap,
  addEdge, useNodesState, useEdgesState,
  type Connection, type Node, type Edge,
  BackgroundVariant,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { StartNode } from "./nodes/start-node"
import { OutputNode } from "./nodes/output-node"
import { BlockNode } from "./nodes/block-node"

const nodeTypes = {
  start: StartNode,
  output: OutputNode,
  block: BlockNode,
}

interface FlowCanvasProps {
  initialNodes: Node[]
  initialEdges: Edge[]
  onSave: (nodes: Node[], edges: Edge[]) => void
}

export function FlowCanvas({ initialNodes, initialEdges, onSave }: FlowCanvasProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges)

  const onConnect = useCallback(
    (connection: Connection) => {
      setEdges((eds) => addEdge({ ...connection, animated: true }, eds))
    },
    [setEdges]
  )

  return (
    <div className="h-full w-full">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={nodeTypes}
        fitView
        proOptions={{ hideAttribution: true }}
        className="bg-background"
      >
        <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="#27272a" />
        <Controls className="!bg-card !border-border !rounded-lg [&>button]:!bg-card [&>button]:!border-border [&>button]:!text-foreground" />
        <MiniMap
          className="!bg-card !border-border !rounded-lg"
          nodeColor="#27272a"
          maskColor="rgba(0,0,0,0.7)"
        />
      </ReactFlow>
    </div>
  )
}
```

- [ ] **Step 5: Create composition editor page**

```tsx
// app/(app)/compositions/[id]/page.tsx
import { db } from "@/lib/db"
import { compositions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, and } from "drizzle-orm"
import { redirect, notFound } from "next/navigation"
import { FlowCanvas } from "@/components/editor/flow-canvas"

export const dynamic = "force-dynamic"

export default async function CompositionEditorPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { orgId } = await auth()
  if (!orgId) redirect("/sign-in")
  const { id } = await params

  const [comp] = await db
    .select()
    .from(compositions)
    .where(and(eq(compositions.id, id), eq(compositions.teamId, orgId)))

  if (!comp) notFound()

  const graph = comp.graph as { nodes: any[]; edges: any[] }

  return (
    <div className="flex h-[calc(100dvh-48px)] flex-col">
      <div className="flex items-center justify-between border-b border-border px-4 py-2">
        <div className="flex items-center gap-3">
          <h1 className="text-sm font-semibold">{comp.name}</h1>
          <span className="rounded bg-success/10 px-2 py-0.5 font-mono text-[10px] text-success">
            v{comp.version}
          </span>
        </div>
      </div>
      <div className="flex-1">
        <FlowCanvas
          initialNodes={graph.nodes}
          initialEdges={graph.edges}
          onSave={async () => {}}
        />
      </div>
    </div>
  )
}
```

- [ ] **Step 6: Verify React Flow renders**

```bash
npm run dev
# Create a composition via API, navigate to /compositions/{id}
# Expected: React Flow canvas with Start and Output nodes, dot grid background, controls, minimap
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: composition editor with React Flow canvas, Start/Output/Block nodes"
```

---

## Task 8: IF Boolean + IF Switch Nodes

**Files:**
- Create: `promptkit/components/editor/nodes/if-boolean-node.tsx`
- Create: `promptkit/components/editor/nodes/if-switch-node.tsx`
- Create: `promptkit/components/editor/nodes/merge-node.tsx`
- Modify: `promptkit/components/editor/flow-canvas.tsx` (register new node types)

- [ ] **Step 1: Create IF Boolean node**

```tsx
// components/editor/nodes/if-boolean-node.tsx
"use client"

import { Handle, Position, type NodeProps } from "@xyflow/react"

interface IfBooleanData {
  field: string
  label?: string
}

export function IfBooleanNode({ data }: NodeProps) {
  const { field, label } = data as IfBooleanData

  return (
    <div className="rounded-lg border-2 border-primary bg-primary/5 px-3 py-2 min-w-[130px]">
      <Handle type="target" position={Position.Left} className="!bg-primary !h-2 !w-2" />
      <div className="flex items-center gap-1.5 mb-1">
        <div className="flex h-4 w-4 items-center justify-center rounded bg-primary">
          <span className="text-[8px] font-bold text-white">IF</span>
        </div>
        <span className="text-[10px] font-semibold text-primary/80">Boolean</span>
      </div>
      <div className="text-xs font-semibold text-foreground">{field || "field"}</div>
      <div className="mt-1.5 flex flex-col gap-1">
        <div className="flex items-center gap-1.5">
          <div className="h-1.5 w-1.5 rounded-full bg-success" />
          <span className="text-[9px] text-success font-mono">== true</span>
        </div>
        <div className="flex items-center gap-1.5">
          <div className="h-1.5 w-1.5 rounded-full bg-destructive" />
          <span className="text-[9px] text-destructive font-mono">== false</span>
        </div>
      </div>
      {/* Two source handles: top for true, bottom for false */}
      <Handle
        type="source"
        position={Position.Right}
        id="true"
        className="!bg-success !h-2 !w-2"
        style={{ top: "40%" }}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="false"
        className="!bg-destructive !h-2 !w-2"
        style={{ top: "70%" }}
      />
    </div>
  )
}
```

- [ ] **Step 2: Create IF Switch node**

```tsx
// components/editor/nodes/if-switch-node.tsx
"use client"

import { Handle, Position, type NodeProps } from "@xyflow/react"

interface IfSwitchData {
  field: string
  cases: string[] // e.g. ["ecommerce", "mobile", "web"]
}

const caseColors = ["#f59e0b", "#06b6d4", "#6b7280", "#ec4899", "#8b5cf6"]

export function IfSwitchNode({ data }: NodeProps) {
  const { field, cases = [] } = data as IfSwitchData

  return (
    <div className="rounded-lg border-2 border-primary bg-primary/5 px-3 py-2 min-w-[150px]">
      <Handle type="target" position={Position.Left} className="!bg-primary !h-2 !w-2" />
      <div className="flex items-center gap-1.5 mb-1">
        <div className="flex h-4 w-4 items-center justify-center rounded bg-primary">
          <span className="text-[7px] font-bold text-white">SW</span>
        </div>
        <span className="text-[10px] font-semibold text-primary/80">Switch</span>
      </div>
      <div className="text-xs font-semibold text-foreground">{field || "field"}</div>
      <div className="mt-1.5 flex flex-col gap-1">
        {cases.map((c, i) => (
          <div key={c} className="flex items-center gap-1.5">
            <div
              className="h-1.5 w-1.5 rounded-sm"
              style={{ background: caseColors[i % caseColors.length] }}
            />
            <span className="text-[8px] font-mono text-muted-foreground">== "{c}"</span>
          </div>
        ))}
      </div>
      {cases.map((c, i) => (
        <Handle
          key={c}
          type="source"
          position={Position.Right}
          id={c}
          className="!h-2 !w-2"
          style={{
            top: `${35 + i * (50 / Math.max(cases.length, 1))}%`,
            background: caseColors[i % caseColors.length],
          }}
        />
      ))}
    </div>
  )
}
```

- [ ] **Step 3: Create Merge node**

```tsx
// components/editor/nodes/merge-node.tsx
"use client"

import { Handle, Position } from "@xyflow/react"

export function MergeNode() {
  return (
    <div className="rounded-md border border-primary/30 bg-primary/5 px-2.5 py-1.5">
      <Handle type="target" position={Position.Left} className="!bg-primary !h-2 !w-2" />
      <span className="text-[9px] font-semibold text-primary/70">MERGE</span>
      <Handle type="source" position={Position.Right} className="!bg-primary !h-2 !w-2" />
    </div>
  )
}
```

- [ ] **Step 4: Register all node types in FlowCanvas**

Add to `components/editor/flow-canvas.tsx`:

```typescript
import { IfBooleanNode } from "./nodes/if-boolean-node"
import { IfSwitchNode } from "./nodes/if-switch-node"
import { MergeNode } from "./nodes/merge-node"

const nodeTypes = {
  start: StartNode,
  output: OutputNode,
  block: BlockNode,
  ifBoolean: IfBooleanNode,
  ifSwitch: IfSwitchNode,
  merge: MergeNode,
}
```

- [ ] **Step 5: Verify IF nodes render on canvas**

Manually add IF nodes to a composition's graph JSON via the API, then visit the editor page. Verify IF Boolean shows true/false handles, IF Switch shows case handles with colors, Merge node shows inline.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: IF Boolean, IF Switch, and Merge nodes for conditional composition"
```

---

## Task 9: Graph Assembly Engine

**Files:**
- Create: `promptkit/lib/graph-engine.ts`

This is the core algorithm — it walks the React Flow graph and assembles the prompt text. Used both server-side (for preview) and in the SDK.

- [ ] **Step 1: Create the graph engine**

```typescript
// lib/graph-engine.ts

interface GraphNode {
  id: string
  type: string
  data: Record<string, any>
}

interface GraphEdge {
  id: string
  source: string
  target: string
  sourceHandle?: string | null
}

interface BlockLookup {
  [blockId: string]: { content: string; name: string }
}

interface AssemblyResult {
  text: string
  blocks: string[]
  tokenCount: number
}

export function assembleGraph(
  nodes: GraphNode[],
  edges: GraphEdge[],
  blocks: BlockLookup,
  context: Record<string, any>
): AssemblyResult {
  const nodeMap = new Map(nodes.map((n) => [n.id, n]))
  const edgesBySource = new Map<string, GraphEdge[]>()
  for (const edge of edges) {
    const key = edge.source
    if (!edgesBySource.has(key)) edgesBySource.set(key, [])
    edgesBySource.get(key)!.push(edge)
  }

  const result: string[] = []
  const resolvedBlocks: string[] = []

  function walk(nodeId: string) {
    const node = nodeMap.get(nodeId)
    if (!node) return

    switch (node.type) {
      case "start":
        // Just proceed to next
        break

      case "block": {
        const blockId = node.data.blockId as string
        const block = blocks[blockId]
        if (block) {
          let content = block.content
          // Interpolate {{variables}}
          content = content.replace(/\{\{(\w+)\}\}/g, (_, key) => {
            return context[key] !== undefined ? String(context[key]) : `{{${key}}}`
          })
          result.push(content)
          resolvedBlocks.push(block.name)
        }
        break
      }

      case "ifBoolean": {
        const field = node.data.field as string
        const value = Boolean(resolveContextValue(context, field))
        // Follow the matching handle
        const handleId = value ? "true" : "false"
        const matchingEdges = (edgesBySource.get(node.id) ?? []).filter(
          (e) => e.sourceHandle === handleId
        )
        for (const edge of matchingEdges) {
          walk(edge.target)
        }
        return // Don't follow default edges
      }

      case "ifSwitch": {
        const field = node.data.field as string
        const value = String(resolveContextValue(context, field))
        const cases = (node.data.cases as string[]) ?? []
        // Find matching case or default
        const matchCase = cases.includes(value) ? value : cases[cases.length - 1]
        const matchingEdges = (edgesBySource.get(node.id) ?? []).filter(
          (e) => e.sourceHandle === matchCase
        )
        for (const edge of matchingEdges) {
          walk(edge.target)
        }
        return
      }

      case "merge":
        // Just proceed to next
        break

      case "output":
        // Terminal — stop
        return
    }

    // Follow all outgoing edges (for non-IF nodes)
    const outEdges = edgesBySource.get(node.id) ?? []
    for (const edge of outEdges) {
      walk(edge.target)
    }
  }

  // Find the start node
  const startNode = nodes.find((n) => n.type === "start")
  if (startNode) walk(startNode.id)

  const text = result.join("\n\n")
  return {
    text,
    blocks: resolvedBlocks,
    tokenCount: Math.round(text.length / 4),
  }
}

function resolveContextValue(context: Record<string, any>, path: string): any {
  // Support nested paths like "_req.country" or "_time.hour"
  const parts = path.split(".")
  let current: any = context
  for (const part of parts) {
    if (current === undefined || current === null) return undefined
    current = current[part]
  }
  return current
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat: graph assembly engine — walks React Flow graph, evaluates IF nodes, returns assembled text"
```

---

## Task 10: SDK Config Endpoint

**Files:**
- Create: `promptkit/app/api/sdk/config/[env]/route.ts`

- [ ] **Step 1: Create SDK config endpoint**

This returns the full config payload for an environment — all compositions, their graphs, and all blocks.

```typescript
// app/api/sdk/config/[env]/route.ts
import { db } from "@/lib/db"
import { blocks, compositions, deployments, apiKeys } from "@/lib/schema"
import { eq, and, desc } from "drizzle-orm"
import { NextResponse } from "next/server"
import crypto from "crypto"

async function authenticateSDK(req: Request) {
  const authHeader = req.headers.get("authorization")
  if (!authHeader?.startsWith("Bearer ")) return null

  const key = authHeader.slice(7)
  const hash = crypto.createHash("sha256").update(key).digest("hex")

  const [apiKey] = await db
    .select()
    .from(apiKeys)
    .where(eq(apiKeys.keyHash, hash))

  return apiKey ?? null
}

export async function GET(
  req: Request,
  { params }: { params: Promise<{ env: string }> }
) {
  const apiKey = await authenticateSDK(req)
  if (!apiKey) return NextResponse.json({ error: "Invalid API key" }, { status: 401 })

  const { env } = await params
  const teamId = apiKey.teamId

  // Get all blocks for this team
  const teamBlocks = await db
    .select()
    .from(blocks)
    .where(eq(blocks.teamId, teamId))

  const blockLookup: Record<string, { name: string; content: string; version: number }> = {}
  for (const b of teamBlocks) {
    blockLookup[b.id] = { name: b.name, content: b.content, version: b.version }
  }

  // Get all compositions for this team
  const teamCompositions = await db
    .select()
    .from(compositions)
    .where(eq(compositions.teamId, teamId))

  // Get latest deployment per composition for this env
  const activeDeployments = await db
    .select()
    .from(deployments)
    .where(eq(deployments.environment, env as "dev" | "staging" | "prod"))
    .orderBy(desc(deployments.deployedAt))

  const deployedVersions = new Map<string, number>()
  for (const d of activeDeployments) {
    if (!deployedVersions.has(d.compositionId)) {
      deployedVersions.set(d.compositionId, d.version)
    }
  }

  const compositionConfigs = teamCompositions.map((c) => ({
    id: c.id,
    name: c.name,
    version: deployedVersions.get(c.id) ?? c.version,
    graph: c.graph,
    contextSchema: c.contextSchema,
  }))

  return NextResponse.json({
    version: Date.now().toString(),
    environment: env,
    blocks: blockLookup,
    compositions: compositionConfigs,
  })
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat: SDK config endpoint — returns full composition + block payload per environment"
```

---

## Task 11: TypeScript SDK

**Files:**
- Create: `sdk/` (entire SDK package)

- [ ] **Step 1: Initialize SDK package**

```bash
mkdir -p sdk/src
cd sdk
npm init -y
npm install -D typescript vitest
```

Create `sdk/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "declaration": true,
    "outDir": "dist",
    "rootDir": "src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true
  },
  "include": ["src"]
}
```

Update `sdk/package.json`:
```json
{
  "name": "@promptkit/sdk",
  "version": "0.1.0",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "scripts": {
    "build": "tsc",
    "test": "vitest run"
  }
}
```

- [ ] **Step 2: Create SDK types**

```typescript
// sdk/src/types.ts
export interface PromptKitConfig {
  apiKey: string
  environment?: string
  baseUrl?: string
  syncIntervalMs?: number
}

export interface ComposeContext {
  [key: string]: any
  _request?: {
    ip?: string
    userId?: string
    userAgent?: string
  }
}

export interface ComposeResult {
  id: string
  text: string
  version: string
  variantId: string | null
  tokenCount: number
  blocks: string[]
  compositionName: string
}

export interface TrackPayload {
  input: string
  output: string
  model?: string
  latencyMs?: number
}

export interface SDKConfig {
  version: string
  environment: string
  blocks: Record<string, { name: string; content: string; version: number }>
  compositions: Array<{
    id: string
    name: string
    version: number
    graph: { nodes: any[]; edges: any[] }
    contextSchema: any[]
  }>
}
```

- [ ] **Step 3: Create compose engine (shared with server)**

```typescript
// sdk/src/compose.ts
import type { SDKConfig, ComposeContext, ComposeResult } from "./types"

export function compose(
  config: SDKConfig,
  compositionName: string,
  context: ComposeContext
): ComposeResult {
  const comp = config.compositions.find((c) => c.name === compositionName)
  if (!comp) throw new Error(`Composition "${compositionName}" not found`)

  const { nodes, edges } = comp.graph
  const nodeMap = new Map(nodes.map((n: any) => [n.id, n]))
  const edgesBySource = new Map<string, any[]>()
  for (const edge of edges) {
    if (!edgesBySource.has(edge.source)) edgesBySource.set(edge.source, [])
    edgesBySource.get(edge.source)!.push(edge)
  }

  // Merge auto-captured context
  const fullContext: Record<string, any> = {
    ...context,
    _time: {
      hour: new Date().getHours(),
      dayOfWeek: new Date().getDay(),
      date: new Date().toISOString().split("T")[0],
      timestamp: new Date().toISOString(),
    },
    _env: { name: config.environment },
    _sdk: { version: "0.1.0", language: "typescript" },
  }
  if (context._request) {
    fullContext._req = context._request
  }

  const parts: string[] = []
  const resolvedBlocks: string[] = []

  function walk(nodeId: string) {
    const node = nodeMap.get(nodeId)
    if (!node) return

    if (node.type === "block") {
      const block = config.blocks[node.data.blockId]
      if (block) {
        let content = block.content
        content = content.replace(/\{\{(\w+)\}\}/g, (_, key: string) =>
          fullContext[key] !== undefined ? String(fullContext[key]) : `{{${key}}}`
        )
        parts.push(content)
        resolvedBlocks.push(block.name)
      }
    } else if (node.type === "ifBoolean") {
      const value = Boolean(resolve(fullContext, node.data.field))
      const handle = value ? "true" : "false"
      for (const e of (edgesBySource.get(node.id) ?? []).filter((e: any) => e.sourceHandle === handle)) {
        walk(e.target)
      }
      return
    } else if (node.type === "ifSwitch") {
      const value = String(resolve(fullContext, node.data.field))
      const cases = node.data.cases ?? []
      const match = cases.includes(value) ? value : cases[cases.length - 1]
      for (const e of (edgesBySource.get(node.id) ?? []).filter((e: any) => e.sourceHandle === match)) {
        walk(e.target)
      }
      return
    }

    for (const e of edgesBySource.get(node.id) ?? []) {
      walk(e.target)
    }
  }

  const start = nodes.find((n: any) => n.type === "start")
  if (start) walk(start.id)

  const text = parts.join("\n\n")
  return {
    id: `asm_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
    text,
    version: `v${comp.version}`,
    variantId: null,
    tokenCount: Math.round(text.length / 4),
    blocks: resolvedBlocks,
    compositionName,
  }
}

function resolve(ctx: Record<string, any>, path: string): any {
  return path.split(".").reduce((o, k) => o?.[k], ctx)
}
```

- [ ] **Step 4: Create main PromptKit client**

```typescript
// sdk/src/client.ts
import type { PromptKitConfig, ComposeContext, ComposeResult, TrackPayload, SDKConfig } from "./types"
import { compose } from "./compose"

export class PromptKit {
  private apiKey: string
  private baseUrl: string
  private environment: string
  private syncInterval: number
  private config: SDKConfig | null = null
  private syncTimer: ReturnType<typeof setInterval> | null = null

  constructor(options: PromptKitConfig) {
    this.apiKey = options.apiKey
    this.baseUrl = options.baseUrl ?? "https://app.promptkit.dev"
    this.environment = options.environment ?? "prod"
    this.syncInterval = options.syncIntervalMs ?? 30_000
  }

  async initialize(): Promise<void> {
    await this.fetchConfig()
    this.syncTimer = setInterval(() => this.fetchConfig(), this.syncInterval)
  }

  private async fetchConfig(): Promise<void> {
    const res = await fetch(`${this.baseUrl}/api/sdk/config/${this.environment}`, {
      headers: { Authorization: `Bearer ${this.apiKey}` },
    })
    if (!res.ok) throw new Error(`PromptKit: config fetch failed (${res.status})`)
    this.config = await res.json()
  }

  async compose(name: string, context: ComposeContext = {}): Promise<ComposeResult> {
    if (!this.config) await this.initialize()
    return compose(this.config!, name, context)
  }

  async track(assemblyId: string, payload: TrackPayload): Promise<void> {
    await fetch(`${this.baseUrl}/api/sdk/track`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ assemblyId, ...payload }),
    })
  }

  async score(assemblyId: string, metrics: Record<string, any>): Promise<void> {
    await fetch(`${this.baseUrl}/api/sdk/score`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ assemblyId, metrics }),
    })
  }

  destroy(): void {
    if (this.syncTimer) clearInterval(this.syncTimer)
  }
}
```

- [ ] **Step 5: Create SDK entry point**

```typescript
// sdk/src/index.ts
export { PromptKit } from "./client"
export type {
  PromptKitConfig,
  ComposeContext,
  ComposeResult,
  TrackPayload,
} from "./types"
```

- [ ] **Step 6: Write SDK tests**

```typescript
// sdk/src/compose.test.ts
import { describe, it, expect } from "vitest"
import { compose } from "./compose"
import type { SDKConfig } from "./types"

const mockConfig: SDKConfig = {
  version: "1",
  environment: "prod",
  blocks: {
    "block-role": { name: "role", content: "You are a senior engineer.", version: 1 },
    "block-design": { name: "design", content: "Design philosophy for {{projectType}}.", version: 1 },
    "block-auth": { name: "auth-rules", content: "JWT auth with bcrypt.", version: 1 },
    "block-mobile": { name: "mobile-rules", content: "Mobile app patterns.", version: 1 },
    "block-web": { name: "web-rules", content: "Web design patterns.", version: 1 },
  },
  compositions: [
    {
      id: "comp-1",
      name: "builder",
      version: 3,
      contextSchema: [],
      graph: {
        nodes: [
          { id: "start", type: "start", position: { x: 0, y: 0 }, data: {} },
          { id: "n-role", type: "block", position: { x: 100, y: 0 }, data: { blockId: "block-role", label: "role" } },
          { id: "n-design", type: "block", position: { x: 200, y: 0 }, data: { blockId: "block-design", label: "design" } },
          { id: "if-auth", type: "ifBoolean", position: { x: 300, y: 0 }, data: { field: "hasAuth" } },
          { id: "n-auth", type: "block", position: { x: 400, y: 0 }, data: { blockId: "block-auth", label: "auth" } },
          { id: "merge-1", type: "merge", position: { x: 500, y: 0 }, data: {} },
          { id: "output", type: "output", position: { x: 600, y: 0 }, data: {} },
        ],
        edges: [
          { id: "e1", source: "start", target: "n-role" },
          { id: "e2", source: "n-role", target: "n-design" },
          { id: "e3", source: "n-design", target: "if-auth" },
          { id: "e4", source: "if-auth", target: "n-auth", sourceHandle: "true" },
          { id: "e5", source: "if-auth", target: "merge-1", sourceHandle: "false" },
          { id: "e6", source: "n-auth", target: "merge-1" },
          { id: "e7", source: "merge-1", target: "output" },
        ],
      },
    },
  ],
}

describe("compose", () => {
  it("assembles blocks in order", () => {
    const result = compose(mockConfig, "builder", { projectType: "web", hasAuth: false })
    expect(result.blocks).toEqual(["role", "design"])
    expect(result.text).toContain("senior engineer")
    expect(result.text).toContain("Design philosophy for web")
    expect(result.text).not.toContain("JWT")
  })

  it("includes conditional block when condition is true", () => {
    const result = compose(mockConfig, "builder", { projectType: "web", hasAuth: true })
    expect(result.blocks).toEqual(["role", "design", "auth-rules"])
    expect(result.text).toContain("JWT auth")
  })

  it("interpolates variables in block content", () => {
    const result = compose(mockConfig, "builder", { projectType: "mobile", hasAuth: false })
    expect(result.text).toContain("Design philosophy for mobile")
  })

  it("returns token count estimate", () => {
    const result = compose(mockConfig, "builder", { projectType: "web", hasAuth: false })
    expect(result.tokenCount).toBeGreaterThan(0)
  })

  it("throws for unknown composition", () => {
    expect(() => compose(mockConfig, "nonexistent", {})).toThrow('Composition "nonexistent" not found')
  })
})
```

- [ ] **Step 7: Run tests**

```bash
cd sdk
npx vitest run
# Expected: All 5 tests pass
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: @promptkit/sdk — compose engine, config sync, track/score API, tests passing"
```

---

## Task 12: Compositions List Page

**Files:**
- Create: `promptkit/app/(app)/compositions/page.tsx`

- [ ] **Step 1: Create compositions list page**

```tsx
// app/(app)/compositions/page.tsx
import { db } from "@/lib/db"
import { compositions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq, desc } from "drizzle-orm"
import { redirect } from "next/navigation"
import Link from "next/link"
import { Plus, GitBranch } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"

export const dynamic = "force-dynamic"

export default async function CompositionsPage() {
  const { orgId } = await auth()
  if (!orgId) redirect("/sign-in")

  const comps = await db
    .select()
    .from(compositions)
    .where(eq(compositions.teamId, orgId))
    .orderBy(desc(compositions.updatedAt))

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-lg font-semibold tracking-tight">Compositions</h1>
        <Button size="sm" className="gap-1.5">
          <Plus className="h-3.5 w-3.5" /> New Composition
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {comps.map((comp) => {
          const graph = comp.graph as { nodes: any[]; edges: any[] }
          const blockCount = graph.nodes.filter((n: any) => n.type === "block").length
          const ifCount = graph.nodes.filter((n: any) => n.type?.startsWith("if")).length

          return (
            <Link
              key={comp.id}
              href={`/compositions/${comp.id}`}
              className="rounded-lg border border-border bg-card p-3 transition-colors hover:border-primary/30"
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-sm font-medium">{comp.name}</span>
                </div>
                <Badge variant="secondary" className="text-[10px]">v{comp.version}</Badge>
              </div>
              <p className="mt-1.5 text-xs text-muted-foreground">
                {blockCount} blocks · {ifCount} IF nodes
              </p>
            </Link>
          )
        })}

        {comps.length === 0 && (
          <p className="col-span-full py-12 text-center text-sm text-muted-foreground">
            No compositions yet. Create your first one.
          </p>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat: compositions list page with block/IF node counts"
```

---

## Task 13: Dashboard Page

**Files:**
- Create: `promptkit/components/dashboard/stat-card.tsx`
- Modify: `promptkit/app/(app)/page.tsx`

- [ ] **Step 1: Create stat card component**

```tsx
// components/dashboard/stat-card.tsx
interface StatCardProps {
  label: string
  value: string | number
  detail?: string
  detailColor?: "success" | "warning" | "muted"
}

export function StatCard({ label, value, detail, detailColor = "muted" }: StatCardProps) {
  const colorMap = {
    success: "text-success",
    warning: "text-warning",
    muted: "text-muted-foreground",
  }

  return (
    <div className="rounded-xl border border-border bg-card p-3">
      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-1 text-2xl font-semibold tracking-tight">{value}</div>
      {detail && (
        <div className={`mt-0.5 text-[10px] ${colorMap[detailColor]}`}>{detail}</div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Create dashboard page with stats**

```tsx
// app/(app)/page.tsx
import { db } from "@/lib/db"
import { blocks, compositions } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq } from "drizzle-orm"
import { redirect } from "next/navigation"
import { StatCard } from "@/components/dashboard/stat-card"
import Link from "next/link"
import { GitBranch } from "lucide-react"
import { Badge } from "@/components/ui/badge"

export const dynamic = "force-dynamic"

export default async function DashboardPage() {
  const { orgId } = await auth()
  if (!orgId) redirect("/sign-in")

  const teamBlocks = await db.select().from(blocks).where(eq(blocks.teamId, orgId))
  const teamComps = await db.select().from(compositions).where(eq(compositions.teamId, orgId))

  const totalTokens = teamBlocks.reduce((sum, b) => sum + Math.round(b.content.length / 4), 0)

  return (
    <div>
      <h1 className="text-lg font-semibold tracking-tight mb-4">Dashboard</h1>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4 mb-6">
        <StatCard label="Compositions" value={teamComps.length} />
        <StatCard label="Blocks" value={teamBlocks.length} detail={`~${totalTokens.toLocaleString()} total tokens`} />
        <StatCard label="Assemblies / 24h" value="—" detail="Connect SDK to start tracking" />
        <StatCard label="Avg Score" value="—" detail="Enable scoring in Phase 2" />
      </div>

      <h2 className="text-sm font-semibold text-muted-foreground mb-3">Compositions</h2>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {teamComps.map((comp) => (
          <Link
            key={comp.id}
            href={`/compositions/${comp.id}`}
            className="rounded-lg border border-border bg-card p-3 transition-colors hover:border-primary/30"
          >
            <div className="flex items-center gap-2">
              <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-sm font-medium">{comp.name}</span>
              <Badge variant="secondary" className="ml-auto text-[10px]">v{comp.version}</Badge>
            </div>
          </Link>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: dashboard page with stat cards and composition quick-access grid"
```

---

## Task 14: Preview Panel

**Files:**
- Create: `promptkit/components/editor/preview-panel.tsx`
- Modify: `promptkit/app/(app)/compositions/[id]/page.tsx` (add preview)

- [ ] **Step 1: Create preview panel**

```tsx
// components/editor/preview-panel.tsx
"use client"

import { useState, useMemo } from "react"
import { assembleGraph } from "@/lib/graph-engine"
import { ChevronUp, ChevronDown } from "lucide-react"

interface PreviewPanelProps {
  nodes: any[]
  edges: any[]
  blocks: Record<string, { name: string; content: string }>
  contextSchema: Array<{ name: string; type: string; values?: string[] }>
}

export function PreviewPanel({ nodes, edges, blocks, contextSchema }: PreviewPanelProps) {
  const [expanded, setExpanded] = useState(true)
  const [context, setContext] = useState<Record<string, any>>({})

  const result = useMemo(() => {
    try {
      return assembleGraph(nodes, edges, blocks, context)
    } catch {
      return { text: "", blocks: [], tokenCount: 0 }
    }
  }, [nodes, edges, blocks, context])

  return (
    <div className="border-t border-border bg-card">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between px-4 py-2 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <span>Preview · {result.tokenCount} tokens · {result.blocks.length} blocks</span>
        {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronUp className="h-3.5 w-3.5" />}
      </button>
      {expanded && (
        <div className="flex border-t border-border">
          {/* Context inputs */}
          <div className="w-48 border-r border-border p-3">
            <div className="text-[9px] uppercase tracking-wider text-muted-foreground mb-2">Test Context</div>
            {contextSchema.map((field) => (
              <div key={field.name} className="mb-2">
                <label className="text-[10px] text-muted-foreground">{field.name}</label>
                {field.type === "boolean" ? (
                  <select
                    className="mt-0.5 w-full rounded border border-border bg-background px-2 py-1 text-xs"
                    value={String(context[field.name] ?? "false")}
                    onChange={(e) => setContext({ ...context, [field.name]: e.target.value === "true" })}
                  >
                    <option value="false">false</option>
                    <option value="true">true</option>
                  </select>
                ) : field.type === "enum" && field.values ? (
                  <select
                    className="mt-0.5 w-full rounded border border-border bg-background px-2 py-1 text-xs"
                    value={context[field.name] ?? field.values[0]}
                    onChange={(e) => setContext({ ...context, [field.name]: e.target.value })}
                  >
                    {field.values.map((v) => (
                      <option key={v} value={v}>{v}</option>
                    ))}
                  </select>
                ) : (
                  <input
                    className="mt-0.5 w-full rounded border border-border bg-background px-2 py-1 text-xs"
                    value={context[field.name] ?? ""}
                    onChange={(e) => setContext({ ...context, [field.name]: e.target.value })}
                  />
                )}
              </div>
            ))}
          </div>
          {/* Assembled text */}
          <div className="flex-1 overflow-y-auto p-3" style={{ maxHeight: 200 }}>
            <pre className="whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-muted-foreground">
              {result.text || "Empty — add blocks to the flow."}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat: live preview panel — shows assembled prompt with context inputs"
```

---

## Task 15: Final Integration + Settings Page

**Files:**
- Create: `promptkit/app/(app)/settings/page.tsx`
- Create: `promptkit/app/api/api-keys/route.ts`

- [ ] **Step 1: Create API key management route**

```typescript
// app/api/api-keys/route.ts
import { db } from "@/lib/db"
import { apiKeys } from "@/lib/schema"
import { auth } from "@clerk/nextjs/server"
import { eq } from "drizzle-orm"
import { NextResponse } from "next/server"
import crypto from "crypto"

export async function GET() {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const keys = await db
    .select({ id: apiKeys.id, name: apiKeys.name, keyPrefix: apiKeys.keyPrefix, environment: apiKeys.environment, createdAt: apiKeys.createdAt })
    .from(apiKeys)
    .where(eq(apiKeys.teamId, orgId))

  return NextResponse.json(keys)
}

export async function POST(req: Request) {
  const { orgId } = await auth()
  if (!orgId) return NextResponse.json({ error: "Unauthorized" }, { status: 401 })

  const { name, environment } = await req.json()

  // Generate a secure API key
  const rawKey = `pk_${environment === "prod" ? "live" : "test"}_${crypto.randomBytes(24).toString("base64url")}`
  const keyHash = crypto.createHash("sha256").update(rawKey).digest("hex")
  const keyPrefix = rawKey.slice(0, 12) + "..."

  await db.insert(apiKeys).values({
    teamId: orgId,
    name,
    keyHash,
    keyPrefix,
    environment,
  })

  // Return the raw key ONCE — it won't be shown again
  return NextResponse.json({ key: rawKey, prefix: keyPrefix }, { status: 201 })
}
```

- [ ] **Step 2: Create settings page**

```tsx
// app/(app)/settings/page.tsx
"use client"

import { useState, useEffect } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Key, Copy, Plus } from "lucide-react"
import { toast } from "sonner"

export default function SettingsPage() {
  const [keys, setKeys] = useState<any[]>([])
  const [newKeyName, setNewKeyName] = useState("")
  const [newKeyEnv, setNewKeyEnv] = useState("dev")
  const [revealedKey, setRevealedKey] = useState<string | null>(null)

  useEffect(() => {
    fetch("/api/api-keys").then((r) => r.json()).then(setKeys)
  }, [])

  async function createKey() {
    const res = await fetch("/api/api-keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: newKeyName, environment: newKeyEnv }),
    })
    const data = await res.json()
    setRevealedKey(data.key)
    setNewKeyName("")
    fetch("/api/api-keys").then((r) => r.json()).then(setKeys)
  }

  return (
    <div className="max-w-2xl">
      <h1 className="text-lg font-semibold tracking-tight mb-4">Settings</h1>

      <section className="mb-8">
        <h2 className="text-sm font-semibold mb-3">API Keys</h2>

        {revealedKey && (
          <div className="mb-4 rounded-lg border border-warning/30 bg-warning/5 p-3">
            <p className="text-xs text-warning font-medium mb-1">Copy this key — it won't be shown again:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded bg-background px-2 py-1 font-mono text-xs">{revealedKey}</code>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => { navigator.clipboard.writeText(revealedKey); toast.success("Copied!") }}
              >
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}

        <div className="space-y-2 mb-4">
          {keys.map((k) => (
            <div key={k.id} className="flex items-center gap-3 rounded-lg border border-border bg-card px-3 py-2">
              <Key className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-sm font-medium">{k.name}</span>
              <code className="font-mono text-xs text-muted-foreground">{k.keyPrefix}</code>
              <span className="ml-auto rounded bg-secondary px-2 py-0.5 text-[10px]">{k.environment}</span>
            </div>
          ))}
        </div>

        <div className="flex items-center gap-2">
          <Input
            placeholder="Key name"
            value={newKeyName}
            onChange={(e) => setNewKeyName(e.target.value)}
            className="w-48"
          />
          <select
            value={newKeyEnv}
            onChange={(e) => setNewKeyEnv(e.target.value)}
            className="rounded border border-border bg-background px-2 py-1.5 text-sm"
          >
            <option value="dev">dev</option>
            <option value="staging">staging</option>
            <option value="prod">prod</option>
          </select>
          <Button size="sm" onClick={createKey} disabled={!newKeyName.trim()} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" /> Create Key
          </Button>
        </div>
      </section>

      <section>
        <h2 className="text-sm font-semibold mb-3">SDK Quick Start</h2>
        <pre className="rounded-lg border border-border bg-card p-4 font-mono text-xs text-muted-foreground overflow-x-auto">
{`npm install @promptkit/sdk

import { PromptKit } from '@promptkit/sdk'

const pk = new PromptKit({
  apiKey: 'pk_live_...',
  environment: 'prod'
})

const result = await pk.compose('builder', {
  projectType: 'ecommerce',
  hasAuth: true
})

console.log(result.text) // assembled prompt`}
        </pre>
      </section>
    </div>
  )
}
```

- [ ] **Step 3: Verify full flow**

```bash
npm run dev
# 1. Sign in with Clerk
# 2. Create blocks (role, design-philosophy, auth-rules)
# 3. Create a composition
# 4. Open the composition editor — see React Flow canvas
# 5. Generate an API key in Settings
# 6. Test SDK config endpoint: curl -H "Authorization: Bearer pk_test_..." localhost:3000/api/sdk/config/dev
```

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat: settings page with API key management and SDK quick start guide"
```

---

## Summary

Phase 1 delivers:
- **15 tasks** covering the full MVP
- Dark zinc UI with sidebar navigation
- Block CRUD with version history
- Composition editor with React Flow (Block, IF Boolean, IF Switch, Merge, Start, Output nodes)
- Graph assembly engine (shared between server preview and SDK)
- TypeScript SDK with `pk.compose()`, config sync, `pk.track()`, `pk.score()`
- SDK config API endpoint
- Dashboard with stats
- Settings with API key management
- Clerk auth with team workspaces

**Next phases** (separate plans):
- Phase 2: Scoring + Experiments (auto-eval, A/B testing, Percentage IF nodes)
- Phase 3: Advanced (Expression nodes, Pipelines, Python SDK, Custom scorers)
- Phase 4: Scale (Edge caching, self-hosting, SSO, audit logs)

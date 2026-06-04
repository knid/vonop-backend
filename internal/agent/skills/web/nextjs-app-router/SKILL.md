---
id: nextjs-app-router
name: Next.js App Router
description: Default Next.js, React, TypeScript, route, metadata, and server/client rules for Wemob web apps.
origin: wemob-builtin
agents:
  - web-builder
project_types:
  - app
  - ecommerce
default: true
triggers:
  - next.js
  - nextjs
  - app router
  - route handler
  - server component
---

## When To Use

Use this skill on every web-builder turn.

## Instructions

- Use Next.js App Router conventions: `app/**/page.tsx`, `layout.tsx`, `route.ts`, `loading.tsx`, `error.tsx`, and server components by default.
- Add `"use client"` only when a component needs browser state, effects, event handlers, refs, client hooks, or client-only libraries.
- Keep data reads in server components and route handlers when possible. Pass concrete data into client components as props.
- Use route handlers for API-style mutations and webhooks. Validate inputs before writing to the database or external services.
- Use `next/image` for important images when dimensions or aspect ratio are known.
- Use metadata exports for public pages. Keep titles and descriptions specific to the app.
- Avoid changing locked core versions for Next, React, TypeScript, or Tailwind.
- If adding a third-party import, add the matching dependency to `package.json` in the same turn and do not edit lockfiles.
- Preserve existing route structure unless the user explicitly asks to reorganize the app.

---
id: web-builder-foundation
name: Web Builder Foundation
description: Default execution discipline for Wemob web and ecommerce builder turns.
origin: wemob-builtin
agents:
  - web-builder
project_types:
  - app
  - ecommerce
default: true
triggers:
  - first_generation
---

## When To Use

Use this skill on every web-builder turn.

## Instructions

- Build the requested product surface first: app shell, routes, forms, data views, and realistic states before marketing sections.
- Keep platform choices within Next.js App Router, React, TypeScript, and Tailwind unless the base prompt explicitly allows another dependency.
- Prefer existing template primitives and helper libraries before adding a new abstraction.
- Preserve working behavior during edits. Read the relevant files before changing routes, shared components, data helpers, auth, checkout, or database code.
- When the task is visual, make the design specific to the user's domain and avoid generic landing-page structure.
- When the task is operational or dashboard-like, prioritize dense, scannable workflows over decorative hero layouts.
- **File batching and size**: Keep each individual file under ~15KB when practical, but batch many small files in one write_files call. If a component is large, split it into smaller modular sub-components (e.g., `TaskTrackerHeader.tsx`, `TaskTrackerList.tsx`) and write those files together instead of one file per round.

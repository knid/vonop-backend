---
id: frontend-patterns
name: Frontend Patterns
description: React, Next.js, state, data fetching, forms, performance, and responsive UI patterns for web apps.
origin: wemob-adapted-from-ecc
agents:
  - web-builder
project_types:
  - app
  - ecommerce
risk_level: low
triggers:
  - react
  - frontend
  - component
  - state
  - form
  - performance
  - dashboard
  - client component
---

## When To Use

Use this skill when building React components, forms, dashboards, stateful UI, data views, or performance-sensitive web surfaces.

## Instructions

- Prefer composition with small components over large components with many mode branches.
- Use server components for data loading by default; add `"use client"` only for state, effects, event handlers, refs, or browser APIs.
- Keep client state local unless multiple distant components need it. Use reducer/context patterns only when prop flow becomes genuinely awkward.
- Build forms with controlled inputs, clear validation, field-level errors, submit loading, and disabled states.
- Keep expensive filtering, sorting, and chart/data transforms out of hot render paths; memoize only when there is real repeated work.
- For long lists or dense tables, use pagination, windowing, or progressive disclosure instead of rendering hundreds of heavy rows.
- Preserve keyboard order, focus states, accessible names, and semantic structure while changing layout.
- Keep responsive layouts stable with grid/flex constraints, min/max widths, aspect ratios, and wrapping text.

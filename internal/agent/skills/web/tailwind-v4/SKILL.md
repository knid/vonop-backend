---
id: tailwind-v4
name: Tailwind v4 UI System
description: Tailwind v4, shadcn/Radix, responsive layout, color, typography, and interaction guidance.
origin: wemob-builtin
agents:
  - web-builder
project_types:
  - app
  - ecommerce
default: true
triggers:
  - tailwind
  - shadcn
  - radix
  - responsive
  - redesign
---

## When To Use

Use this skill on every web-builder turn, especially for UI creation and visual edits.

## Instructions

- Use Tailwind v4 patterns and semantic design tokens where the template provides them.
- Prefer existing shadcn/Radix-style primitives and `cn()` helpers before creating new component APIs.
- Use `lucide-react` icons for recognizable tool and action buttons.
- Keep layouts responsive through stable grid tracks, flex behavior, aspect ratios, min/max constraints, and text wrapping.
- Do not scale font size directly with viewport width. Avoid negative letter spacing.
- Make buttons, inputs, cards, tabs, tables, menus, dialogs, and forms feel like real product UI with loading, empty, error, hover, focus, and disabled states.
- Avoid one-note color palettes. Use contrast, neutral structure, and purposeful accents instead of generic gradients.
- Do not let text or UI elements overlap at mobile or desktop sizes.
- Use cards for repeated items, modals, and framed tools. Do not nest cards or turn every page section into a floating card.

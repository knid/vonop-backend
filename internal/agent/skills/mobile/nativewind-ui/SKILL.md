---
id: nativewind-ui
name: NativeWind Mobile UI
description: NativeWind, react-native-reusables, mobile layout, gestures, accessibility, and performance guidance.
origin: wemob-builtin
agents:
  - mobile-builder
project_types:
  - mobile_app_native
default: true
triggers:
  - nativewind
  - react-native-reusables
  - mobile ui
  - redesign
---

## When To Use

Use this skill on every mobile-builder turn, especially for screen creation and UI edits.

## Instructions

- Use NativeWind classes and existing react-native-reusables-style primitives from the template.
- Design for the smallest supported phone first, then adapt gracefully to larger devices.
- Keep primary actions reachable, usually in the lower portion of the screen or a clear sticky footer.
- Use stable spacing, touch targets of at least 44pt, readable contrast, and dynamic-type-friendly text.
- Provide loading, empty, error, offline, and permission-denied states where the flow can encounter them.
- Use platform-appropriate motion sparingly. Prefer one clear entrance/transition pattern over many competing effects.
- Avoid heavy work in render paths. Memoize expensive list items and keep scroll interactions smooth.
- Set meaningful accessibility labels, roles, and hints where visible text alone is not enough.
- Do not create web-style dense pages; mobile screens should be task-focused and thumb-friendly.

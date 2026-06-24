---
id: expo-react-native-best-practices
name: Expo React Native Best Practices
description: Expo, React Native, app performance, platform behavior, and preview-safe mobile UI guidance.
origin: wemob-mobile-adapted
agents:
  - mobile-builder
project_types:
  - mobile_app_native
risk_level: low
triggers:
  - expo best practices
  - react native best practices
  - mobile performance
  - preview-safe mobile UI
---

## When To Use

Use this skill when building or changing Expo React Native screens, mobile app flows, or performance-sensitive UI.

## Instructions

- Build with native primitives and Expo Router conventions. Do not introduce DOM elements, browser-only APIs, or Next.js assumptions.
- Keep screens resilient across small phones, large phones, safe areas, keyboard overlap, and platform font scaling.
- Use `FlatList` or `SectionList` for long lists and keep item renderers lightweight.
- Keep network flows mobile-aware: loading, retry, stale data, optimistic updates only when rollback is clear.
- Do not add production native services, bridge configuration, app-store build flows, backend/auth infrastructure, push, IAP, or permission-gated native features.
- Avoid heavy work in render paths. Memoize expensive items and debounce high-frequency input when needed.

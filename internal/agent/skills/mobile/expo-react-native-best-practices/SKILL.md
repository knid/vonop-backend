---
id: expo-react-native-best-practices
name: Expo React Native Best Practices
description: Expo, React Native, app performance, platform behavior, native capabilities, and mobile backend integration guidance.
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
  - mobile backend
  - native capability
  - offline
  - app build
---

## When To Use

Use this skill when building or changing Expo React Native screens, mobile app flows, performance-sensitive UI, native capabilities, or mobile backend integration.

## Instructions

- Build with native primitives and Expo Router conventions. Do not introduce DOM elements, browser-only APIs, or Next.js assumptions.
- Keep screens resilient across small phones, large phones, safe areas, keyboard overlap, and platform font scaling.
- Use `FlatList` or `SectionList` for long lists and keep item renderers lightweight.
- Keep network flows mobile-aware: loading, retry, offline, stale data, optimistic updates only when rollback is clear.
- Check mobile bridge capabilities before using camera, location, contacts, push, biometrics, local notifications, calendar, document picker, sharing, or IAP.
- Keep permission prompts specific, minimal, and tied to a visible user action.
- Avoid heavy work in render paths. Memoize expensive items and debounce high-frequency input when needed.
- Treat backend responses as versioned contracts. Mobile clients may be old, offline, retried, or interrupted mid-flow.

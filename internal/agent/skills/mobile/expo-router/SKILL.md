---
id: expo-router
name: Expo Router Native App Structure
description: Expo Router, React Native, navigation, native-platform, and mobile template rules.
origin: wemob-builtin
agents:
  - mobile-builder
project_types:
  - mobile_app_native
default: true
triggers:
  - expo
  - expo router
  - react native
  - native app
  - navigation
---

## When To Use

Use this skill on every mobile-builder turn.

## Instructions

- Use Expo Router file conventions under the mobile app tree: route groups, `_layout.tsx`, screen files, and typed navigation patterns already present in the template.
- Build native React Native screens, not DOM or web-only components. Do not use `window`, `document`, HTML tags, CSS media queries, or Next.js APIs.
- Use Expo Router route conventions and the preview-safe helpers already present in the template.
- Do not add production native services, bridge configuration, app-store build flows, backend/auth infrastructure, push, IAP, or permission-gated native features.
- Use `FlatList` or `SectionList` for long lists instead of mapping large arrays inside `ScrollView`.
- Keep route changes small during edits.

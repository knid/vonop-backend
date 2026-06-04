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
- Use mobile template helpers for API calls, auth, haptics, push, location, camera, contacts, calendar, local notifications, biometrics, review prompts, sharing, network state, and IAP.
- Check or configure mobile bridge capabilities before using native features that require permissions or native setup.
- Use `FlatList` or `SectionList` for long lists instead of mapping large arrays inside `ScrollView`.
- Keep route changes small during edits. Preserve existing navigation groups and auth boundaries unless the user asks to change them.
- For build failures, read logs before editing and prefer the smallest targeted fix.

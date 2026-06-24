---
id: mobile-builder-foundation
name: Mobile Builder Foundation
description: Default execution discipline for Wemob Expo native builder turns.
origin: wemob-builtin
agents:
  - mobile-builder
project_types:
  - mobile_app_native
default: true
triggers:
  - first_generation
---

## When To Use

Use this skill on every mobile-builder turn.

## Instructions

- Build native Expo screens and flows, not a web fallback.
- Stay inside Expo Router, React Native, NativeWind, and the template's installed libraries unless the base prompt explicitly allows another dependency.
- Do not add production native services, bridge configuration, app-store build flows, backend/auth infrastructure, push, IAP, or permission-gated native features.
- Keep screens ergonomic on small devices: reachable controls, clear hierarchy, stable spacing, and no text overlap.
- Prefer platform-appropriate gestures, loading states, empty states, and error states.
- Preserve template-managed native configuration and helper files unless the prompt or tool result says they need to change.
- Import React hooks (`useState`, `useEffect`, `useCallback`, `useMemo`, `useRef`, `useContext`, `useReducer`) from `"react"`, never from `"react-native"` or `"react-native-web"`. Import React Native primitives (`View`, `Text`, `ScrollView`, `FlatList`, etc.) from `"react-native"`.

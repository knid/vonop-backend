---
id: app-store-readiness
name: App Store Readiness
description: Mobile release, permissions, privacy, and build-readiness guidance.
origin: wemob-builtin
agents:
  - mobile-builder
project_types:
  - mobile_app_native
triggers:
  - app store
  - play store
  - testflight
  - release
  - permissions
  - privacy
  - build failed
  - fix build
---

## When To Use

Use this skill when the user asks about mobile release readiness, TestFlight, Play Store, permissions, privacy, or build failures.

## Instructions

- Check current mobile config before changing permission-sensitive features.
- Keep permission prompts specific to the real feature that needs access.
- Avoid enabling camera, contacts, location, push, biometrics, IAP, or background behavior unless the user request needs it.
- If a build failed, inspect build logs first, then make the smallest targeted code or config change.
- After a build-fix edit, use the build trigger tool when available and appropriate so the user gets a fresh validation loop.
- Keep store-facing copy concise, non-technical, and aligned with the app's actual behavior.

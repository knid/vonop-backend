---
id: verification-loop
name: Verification Loop
description: Build, type, lint, test, and review checks for app-builder changes.
origin: wemob-adapted-from-ecc
agents:
  - web-builder
  - mobile-builder
project_types:
  - app
  - ecommerce
  - mobile_app_native
risk_level: low
triggers:
  - verify
  - verification
  - test
  - tests
  - build failed
  - fix build
  - quality gate
  - ready to ship
---

## When To Use

Use this skill after meaningful edits, before claiming a fix is complete, or when the user asks to verify, test, fix a build, or prepare for release.

## Instructions

- Run the narrowest useful checks first: type check, lint, unit tests, build, or platform-specific build logs depending on the change.
- For web apps, prefer project scripts from `package.json` before inventing commands.
- For mobile apps, inspect mobile config and build logs before editing native-sensitive settings.
- If a check fails, fix the smallest root cause and rerun the failed check before moving on.
- Review changed files for accidental route changes, broken imports, missing error states, unsafe user input handling, and text overlap risk.
- When checks are unavailable or too expensive, state exactly what was not run and why.
- Keep the final report concise: checks run, pass/fail result, files changed, and any remaining risk.

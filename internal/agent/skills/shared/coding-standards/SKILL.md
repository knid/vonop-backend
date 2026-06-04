---
id: coding-standards
name: Coding Standards
description: Shared TypeScript, React, React Native, and backend code quality guidance.
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
  - code quality
  - coding standards
  - clean code
  - refactor
  - typescript
  - maintainable
---

## When To Use

Use this skill when the request asks for cleaner code, refactoring, TypeScript correctness, maintainability, or broad app implementation quality.

## Instructions

- Prefer small, typed functions and components with clear props, return types where helpful, and explicit domain names.
- Keep data validation close to external boundaries: route handlers, form submission, API client inputs, and webhook payloads.
- Avoid broad rewrites. Preserve existing routes, component APIs, state shape, and template conventions unless the user asks for a structural change.
- Handle loading, empty, error, disabled, and success states when a UI can naturally encounter them.
- Keep async code explicit: surface errors, avoid swallowed promises, and do not rely on side effects hidden in render paths.
- Prefer existing helpers, primitives, and local patterns before adding a new abstraction or dependency.
- Use comments only when they explain a non-obvious decision, not when they restate the code.
- Do not introduce dead code, placeholder TODOs, fake integrations, or unused dependencies.

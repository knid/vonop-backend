---
id: backend-patterns
name: Backend Patterns
description: Backend, route handler, service, validation, caching, auth, and integration patterns for generated apps.
origin: wemob-adapted-from-ecc
agents:
  - web-builder
  - mobile-builder
project_types:
  - app
  - ecommerce
  - mobile_app_native
risk_level: medium
triggers:
  - backend
  - api
  - route handler
  - server
  - webhook
  - auth
  - integration
  - mobile backend
---

## When To Use

Use this skill when building or changing backend behavior for a web app, ecommerce app, or mobile app backend.

## Instructions

- Keep backend boundaries explicit: parse request input, validate it, authorize it, perform the domain action, then return a clear response.
- Put reusable business logic in helpers or service modules when multiple routes need it; avoid duplicating validation and persistence logic.
- Treat all client input as untrusted, including mobile app requests, query params, headers, webhook bodies, and form data.
- Use consistent error responses that do not leak secrets, internal stack traces, or third-party credentials.
- Add idempotency or duplicate handling for webhooks, payment events, build callbacks, and mobile retry flows.
- Prefer server-side data reads and mutations over client-side bypasses when auth, billing, or ownership matters.
- Cache only stable or explicitly invalidated data. Do not cache user-specific data without a clear key and permission boundary.
- Keep mobile backend APIs version-tolerant so older app builds do not break when backend fields evolve.

---
id: api-design
name: API Design
description: Request, response, pagination, validation, versioning, and error-shape guidance for app APIs.
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
  - api design
  - endpoint
  - route handler
  - rest
  - pagination
  - mobile api
  - response shape
---

## When To Use

Use this skill when designing or modifying endpoints, request payloads, response payloads, pagination, filtering, or error contracts.

## Instructions

- Design endpoints around resources and user actions, not implementation details.
- Validate request bodies, route params, query params, and enum values before using them.
- Return stable response shapes. For mobile clients, avoid removing or renaming fields without a compatibility plan.
- Use pagination for lists that can grow. Include enough metadata for the client to request the next page.
- Keep errors structured: code, message, and optional field errors. Avoid exposing raw provider or database errors.
- Use appropriate HTTP methods and status codes for web route handlers; for tool-like mobile actions, keep success and failure contracts explicit.
- Document or infer whether a response is user-specific, public, cacheable, or mutation-triggering.

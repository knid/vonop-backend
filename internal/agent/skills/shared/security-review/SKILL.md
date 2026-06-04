---
id: security-review
name: Security Review
description: Lightweight app security checks for auth, secrets, injection, payments, webhooks, and mobile backend flows.
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
  - security
  - secure
  - vulnerability
  - auth
  - permissions
  - secret
  - payment
  - webhook
---

## When To Use

Use this skill when touching authentication, authorization, secrets, payments, webhooks, user data, admin actions, or mobile backend APIs.

## Instructions

- Check that every protected read and mutation enforces the correct user, organization, or project ownership boundary.
- Do not expose secrets in client bundles, logs, error messages, generated files, or seed data.
- Validate and sanitize external input before database writes, provider calls, redirects, or file/path operations.
- Verify webhook authenticity before trusting event content.
- For payments and subscriptions, trust provider-confirmed server events more than client success screens.
- Use least privilege for generated API keys, tokens, and third-party integrations.
- Avoid open redirects, broad CORS, unsafe HTML insertion, and path traversal.
- For mobile APIs, assume old app builds, retries, offline sync, and tampered client requests are possible.

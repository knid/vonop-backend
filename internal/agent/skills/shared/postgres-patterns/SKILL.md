---
id: postgres-patterns
name: Postgres Patterns
description: Postgres schema, query, indexing, transaction, and migration guidance for app data.
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
  - postgres
  - database
  - sql
  - schema
  - migration
  - index
  - query
---

## When To Use

Use this skill when changing app data models, writing SQL, adding indexes, designing seed data, or touching database-backed workflows.

## Instructions

- Model ownership and tenant boundaries explicitly. Every user-owned or organization-owned row should have a clear access path.
- Use constraints for invariants the database can enforce: primary keys, unique keys, foreign keys, not-null, and checks.
- Add indexes for frequent lookup, join, sort, and filter patterns; avoid speculative indexes that no query uses.
- Keep transactions small and purposeful. Use them when multiple writes must succeed or fail together.
- Avoid N+1 query patterns. Batch reads or query the needed shape directly.
- Make migrations reversible or clearly safe to replay when the surrounding migration framework supports it.
- Separate seed/demo data from production data assumptions.
- Never place secrets, access tokens, or payment credentials in ordinary app tables unless they use the repo's credential storage pattern.

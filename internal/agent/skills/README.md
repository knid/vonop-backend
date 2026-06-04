# Agent Skills

Wemob builder agents load local `SKILL.md` files with YAML frontmatter. Public
skills can be imported by copying or normalizing them into this shape, then
pinning the source in `origin`.

```yaml
---
id: web-seo
name: Web SEO
description: Search and social-preview guidance for web apps.
origin: wemob-builtin
agents:
  - web-builder
project_types:
  - app
  - ecommerce
default: false
risk_level: low
allowed_tools:
  - read_files
  - grep
triggers:
  - seo
  - metadata
---
```

Required:

- `id`
- Markdown body after the frontmatter

Recommended:

- `agents` so web and mobile builders do not load each other's skills
- `project_types` when a skill is only valid for `app`, `ecommerce`, or
  `mobile_app_native`
- `origin` for imported/public skills, ideally including repository and commit
  or release reference
- `triggers` for turn-scoped activation
- `risk_level` (`low`, `medium`, `high`) for review/import policy
- `allowed_tools` when a skill should constrain or document the expected tool
  surface

Default skills are attached to every compatible turn. Triggered skills are
attached when a slash command or user message requests them.

Imported skills must pass the skill linter before becoming available. Wemob
rejects dynamic shell injection in skill bodies; supporting scripts can be added
later behind an explicit review and execution policy.

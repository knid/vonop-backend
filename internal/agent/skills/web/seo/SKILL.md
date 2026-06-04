---
id: web-seo
name: Web SEO
description: Search, social preview, sitemap, robots, semantic headings, and structured-data guidance for web apps.
origin: wemob-builtin
agents:
  - web-builder
project_types:
  - app
  - ecommerce
triggers:
  - seo
  - search engine
  - metadata
  - open graph
  - sitemap
  - robots.txt
---

## When To Use

Use this skill when the user asks for SEO, metadata, social preview cards, indexability, semantic headings, sitemap, robots.txt, canonical URLs, or structured data.

## Instructions

- Add or update route metadata with specific titles and descriptions. Avoid vague placeholders.
- Include Open Graph and Twitter card metadata when the app has public-facing pages.
- Use semantic heading order and descriptive link text.
- Add `app/sitemap.ts` and `app/robots.ts` when appropriate for the app.
- For ecommerce products, articles, venues, recipes, events, organizations, or local businesses, add JSON-LD only when the page has enough concrete data to support it.
- Do not change visual design or business logic unless the user explicitly asked for a broader redesign.

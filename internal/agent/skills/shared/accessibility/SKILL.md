---
id: accessibility
name: Accessibility
description: Practical accessibility pass for web and mobile UI changes.
origin: wemob-builtin
agents:
  - web-builder
  - mobile-builder
triggers:
  - accessibility
  - accessible
  - a11y
  - aria
  - screen reader
  - keyboard
---

## When To Use

Use this skill when the user asks for accessibility, a11y, keyboard support, screen-reader support, color contrast, labels, or inclusive interaction states.

## Instructions

- Prefer semantic elements and platform-native accessibility APIs before adding ARIA.
- Ensure interactive controls have accessible names, visible focus or pressed states, and sufficient target size.
- Keep text contrast readable against its actual background.
- Do not hide important content from assistive technology.
- For web, preserve keyboard navigation order and use labels for form controls.
- For mobile, set meaningful accessibility labels, roles, and hints where the visual text is not enough.
- Preserve existing visual intent unless the accessibility issue requires a visible adjustment.

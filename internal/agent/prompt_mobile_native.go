package agent

// BasePromptMobileNativeFirstGen is appended to the mobile profile base prompt
// for initial app generation runs (project.Status == "draft").
const BasePromptMobileNativeFirstGen = `## Work order: home screen first (CRITICAL)
On a new project, your VERY FIRST turn MUST customize mobile/app/(app)/index.tsx.
It must render a real home experience: header, hero/intro, primary CTA, and at
least one content block — even with stub data. Other screens come AFTER.

Why: runs can be cancelled mid-loop. If home lands first, the user sees
something working. Treat this as a hard ordering constraint.

This rule does NOT apply when the FILE TREE already shows a customized home.

## Work order: UI first, wiring second (CRITICAL)
After home lands, finish the entire visible UI surface BEFORE any backend wiring.
Use inline stub data so each screen renders a complete, polished experience.

Run order:
  1. mobile/app/(app)/index.tsx                  (Home First)
  2. Every other screen + component with stubs   (UI First)
  3. Image generation                            (Images Last)
  4. Database / native modules / integrations    (only if requested)

This rule does NOT apply when the user explicitly asks for backend work first.

## Work order: images last
Write all screen structure and content first. Call generate_image only after
every write_files and edit_file is complete. Use __IMG_<SLOT>__ placeholder
tokens, then swap them in a final edit_file pass. Never leave placeholders.

`

// BasePromptMobileNativeChat is appended to the mobile profile base prompt for
// chat-mode edit runs.
const BasePromptMobileNativeChat = `# EDITING DISCIPLINE
You are editing an existing mobile project. Make minimal, targeted changes that
address the user's request. Preserve existing functionality, design patterns,
and code style. Do not rewrite entire files unless the user explicitly asks
for a redesign. Use multi_edit when making 2+ changes to the same file. For
large new files, use smaller structured write_files calls instead of one huge
payload.
Respect the existing file structure — do not create new files unless needed.
When the user asks to change an image, replace the existing source directly;
only use __IMG_<SLOT>__ placeholders when adding a brand-new image.
`

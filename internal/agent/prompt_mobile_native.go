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
every write_files and edit_files is complete. Use __IMG_<SLOT>__ placeholder
tokens, then swap them in a final edit_files pass. Never leave placeholders.

`

// BasePromptMobileNativeChat is appended to the mobile profile base prompt for
// chat-mode edit runs.
const BasePromptMobileNativeChat = `# EDITING DISCIPLINE
You are editing an existing mobile project. Make minimal, targeted changes that
address the user's request. Preserve existing functionality, design patterns,
and code style. Do not rewrite entire files unless the user explicitly asks
for a redesign. Use edit_files for existing files and write_files for new
files. Put multiple replacements for the same file in one edit_files file
entry. For large new files, use raw file blocks instead of huge write_files JSON:
BEGIN_WEMOB_FILE path=mobile/example.tsx
raw file content
END_WEMOB_FILE
Raw file blocks are create-only; never use them to edit existing files.
The only file content tools are read_files, write_files, and edit_files.
Respect the existing file structure — do not create new files unless needed.
When the user asks to change an image, replace the existing source directly;
only use __IMG_<SLOT>__ placeholders when adding a brand-new image.
`

package constants

// ai_validation.go contains the LLM-based TypeScript validation prompt.

const NEXTJS_VALIDATE_PROMPT = `You are an expert TypeScript and Next.js 16 type checker. Review the following project files for TypeScript errors, missing imports, and build-time issues.

Focus on:
1. TypeScript type errors (wrong types, missing properties, invalid generics)
2. Missing or incorrect imports (importing from non-existent files, wrong export names, named vs default import mismatch — e.g., using "import { Button }" when the target file has "export default function Button")
3. Next.js-specific issues (incorrect font usage, missing "use client" for hooks, invalid metadata)
4. Tailwind CSS @theme issues (wrong color tokens, invalid @theme syntax)
5. Package.json issues (missing required dependencies)

Pre-installed npm packages (do NOT flag imports of these as errors): lucide-react, clsx, tailwind-merge, class-variance-authority, framer-motion, @radix-ui/react-* (slot, dialog, dropdown-menu, tabs, tooltip, avatar, separator, scroll-area, switch, select, accordion, popover).

Common Next.js pitfalls to check:
- Using .variable on a font that was NOT created with a variable option (e.g., Racing_Sans_One({ subsets: ['latin'] }) does NOT have .variable, you need Racing_Sans_One({ subsets: ['latin'], variable: '--font-racing' }))
- Non-variable Google Fonts (Bebas Neue, Lobster, Pacifico, Permanent Marker, Alfa Slab One, Abril Fatface, etc.) REQUIRE an explicit weight parameter. Missing weight causes "Missing weight for X" build error. Fix: add weight: "400" (or the appropriate available weight) to the font constructor.
- Importing server-only modules in "use client" components
- Using hooks (useState, useEffect) or framer-motion without "use client" directive
- Importing from LOCAL project paths (@/ or ./) that don't exist in the project (do NOT flag npm package imports)

### Output format (strict JSON, no explanation, no markdown wrapping)
If there are errors:
{"valid": false, "errors": [{"file": "app/layout.tsx", "line": 26, "error": "Property 'variable' does not exist on NextFont. Add variable option to font constructor.", "fix": "Change Racing_Sans_One({ subsets: ['latin'] }) to Racing_Sans_One({ subsets: ['latin'], variable: '--font-racing' })"}]}

If everything looks correct:
{"valid": true, "errors": []}`

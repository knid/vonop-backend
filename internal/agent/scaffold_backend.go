package agent

// scaffoldMinimalBackend writes a minimum-viable Next.js App Router
// backend under backend/ so package.json editors can add dependencies
// and the eventual Fly deploy can `next build`. The agent layers actual
// route handlers (backend/app/api/<feature>/route.ts) on top from there.
//
// Callers must check rc.FileMap.Get("backend/package.json") first and
// only call this when the file is absent.
//
// Files written:
//   - backend/package.json — minimal deps (next, react, react-dom).
//   - backend/tsconfig.json — Next.js App Router config with the @/* alias
//     so `import { ... } from '@/lib/...'` resolves.
//   - backend/next.config.mjs — standalone output for Fly deploy.
//
// Dockerfile is intentionally NOT written here — the deploy worker
// auto-writes a default standalone Next.js Dockerfile if missing (P1.1.D).
func scaffoldMinimalBackend(rc *RunContext) {
	files := map[string]string{
		"backend/package.json": `{
  "name": "backend",
  "version": "0.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start"
  },
  "dependencies": {
    "next": "^15.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "typescript": "^5.4.0",
    "@types/node": "^20.0.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0"
  }
}
`,
		"backend/tsconfig.json": `{
  "compilerOptions": {
    "lib": ["dom", "dom.iterable", "esnext"],
    "allowJs": true,
    "skipLibCheck": true,
    "strict": true,
    "noEmit": true,
    "esModuleInterop": true,
    "module": "esnext",
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "isolatedModules": true,
    "jsx": "react-jsx",
    "incremental": true,
    "plugins": [{ "name": "next" }],
    "paths": { "@/*": ["./*"] }
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx", ".next/types/**/*.ts"],
  "exclude": ["node_modules"]
}
`,
		"backend/next.config.mjs": `/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  typescript: { ignoreBuildErrors: true },
};

export default nextConfig;
`,
	}
	for path, content := range files {
		rc.FileMap.Set(path, content)
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      path,
			Operation: "create",
			Bytes:     len(content),
			Content:   content,
		})
	}
}

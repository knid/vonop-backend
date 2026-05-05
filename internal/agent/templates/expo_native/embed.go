// Package expo_native vendors the wemob-expo-template baseline and exposes
// it as an embed.FS. wemob-expo-template is a private repo; rather than
// pulling at runtime, we copy its files into this directory and embed them
// into the binary at build time.
//
// To refresh: run scripts/refresh-expo-template.sh from the repo root.
//
// Used by Controller.scaffoldExpoTemplate at project creation time —
// every mobile_app_native project's git repo gets an opinionated Expo
// SDK 54 + Expo Router + NativeWind v4 + Tailwind v3 baseline under
// mobile/ so the AI agent can read existing files (lib/api.ts,
// lib/auth.ts, components/ui/*) and the preview sandbox can run
// `npx expo start --web` with no extra setup.
package expo_native

import "embed"

// FS contains every file in the vendored template directory except this
// embed.go file itself. The `all:` prefix keeps dotfiles included
// (e.g. .npmrc, .gitignore).
//
// IMPORTANT: every file the user's project needs to boot must appear in
// this directive. Files on disk that are not listed here are silently
// dropped from the binary and will be missing from scaffolded projects —
// past omissions of tailwind.config.js / babel.config.js broke Metro at
// startup with "Cannot find module './tailwind.config'". When refreshing
// the template, sync this list with `ls` of the template root.
//
//go:embed all:app all:assets all:components all:lib all:plugins all:store
//go:embed app.json babel.config.js components.json eas.json global.css
//go:embed metro.config.js nativewind-env.d.ts package.json postcss.config.mjs
//go:embed tailwind.config.js tsconfig.json .npmrc .gitignore
var FS embed.FS

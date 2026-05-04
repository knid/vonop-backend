// Package expo_native vendors the wemob-expo-template baseline and exposes
// it as an embed.FS. wemob-expo-template is a private repo; rather than
// pulling at runtime, we copy its files into this directory and embed them
// into the binary at build time.
//
// To refresh: run scripts/refresh-expo-template.sh from the repo root.
//
// Used by Controller.scaffoldExpoTemplate at project creation time —
// every mobile_app_native project's git repo gets an opinionated Expo
// SDK 54 + Expo Router + NativeWind v4 + react-native-reusables baseline
// under mobile/ so the AI agent can read existing files (lib/api.ts,
// lib/auth.ts, components/ui/*) and the preview sandbox can run
// `npx expo start --web` with no extra setup.
package expo_native

import "embed"

// FS contains every file in the vendored template directory except this
// embed.go file itself. The build tags `all:` keep dotfiles included
// (e.g. nativewind-env.d.ts is fine, but if we add a .env.example later
// it would be skipped without `all:`).
//
//go:embed all:app all:assets all:components all:lib
//go:embed app.json babel.config.js global.css metro.config.js
//go:embed nativewind-env.d.ts package.json tailwind.config.js tsconfig.json
var FS embed.FS

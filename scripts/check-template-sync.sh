#!/usr/bin/env bash
# Verify the embedded Expo template under
# internal/templates/expo_native/ is in sync with the standalone
# wemob-expo-template repo (the scaffold Codemagic clones at build time).
#
# These two trees can drift silently because the agent reads the embedded
# copy while builds run against the standalone repo — see P0 #3 of the
# 2026-05-02 mobile pipeline audit. This script does a `diff -rq` and
# exits non-zero if any file differs (excluding intentional non-ports
# like codemagic.yaml, README.md, embed.go, etc.).
#
# Usage:
#   scripts/check-template-sync.sh                # uses ../wemob-expo-template
#   WEMOB_EXPO_TEMPLATE_PATH=/path scripts/check-template-sync.sh
#
# Not currently wired into CI — run manually after editing either tree.
# To resync standalone → embedded run scripts/refresh-expo-template.sh.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DEFAULT="${REPO_ROOT}/../wemob-expo-template"
SOURCE="${WEMOB_EXPO_TEMPLATE_PATH:-$SOURCE_DEFAULT}"
DEST="${REPO_ROOT}/internal/templates/expo_native"

if [[ ! -d "$SOURCE" ]]; then
  echo "error: wemob-expo-template not found at $SOURCE" >&2
  echo "       set WEMOB_EXPO_TEMPLATE_PATH to point at your local clone" >&2
  exit 2
fi

if [[ ! -d "$DEST" ]]; then
  echo "error: embedded template not found at $DEST" >&2
  exit 2
fi

# Files/dirs deliberately not vendored into the embedded copy — these
# match the exclusions in scripts/refresh-expo-template.sh, plus
# embed.go (lives only in the embedded copy by design).
DIFF=$(diff -rq \
  --exclude=node_modules \
  --exclude=.expo \
  --exclude=.git \
  --exclude=.worktrees \
  --exclude=dist \
  --exclude=package-lock.json \
  --exclude=codemagic.yaml \
  --exclude=.gitignore \
  --exclude=README.md \
  --exclude=expo-env.d.ts \
  --exclude=embed.go \
  "$SOURCE" "$DEST" || true)

if [[ -z "$DIFF" ]]; then
  echo "OK: embedded template is in sync with $SOURCE"
  exit 0
fi

echo "DRIFT detected between standalone and embedded Expo template:" >&2
echo "" >&2
echo "$DIFF" >&2
echo "" >&2
echo "To resync standalone -> embedded:" >&2
echo "  $REPO_ROOT/scripts/refresh-expo-template.sh" >&2
exit 1

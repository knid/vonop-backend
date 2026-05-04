#!/usr/bin/env bash
# Refresh the vendored wemob-expo-template files under
# internal/agent/templates/expo_native/. Run after pulling a new version
# of wemob-expo-template; commit the updated files alongside the bump.
#
# The vendored copy is what gets embedded into the wemob-backend binary
# via //go:embed and copied into every new mobile_app_native project's
# git repo at creation time. Without a refresh, projects scaffold against
# a stale baseline.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DEFAULT="${REPO_ROOT}/../wemob-expo-template"
SOURCE="${WEMOB_EXPO_TEMPLATE_PATH:-$SOURCE_DEFAULT}"
DEST="${REPO_ROOT}/internal/agent/templates/expo_native"

if [[ ! -d "$SOURCE" ]]; then
  echo "error: wemob-expo-template not found at $SOURCE" >&2
  echo "       set WEMOB_EXPO_TEMPLATE_PATH to point at your local clone" >&2
  exit 1
fi

# Preserve embed.go (the //go:embed directive file). Wipe everything else
# so deletions in the upstream template propagate.
EMBED_FILE="$DEST/embed.go"
EMBED_BACKUP=""
if [[ -f "$EMBED_FILE" ]]; then
  EMBED_BACKUP="$(mktemp)"
  cp "$EMBED_FILE" "$EMBED_BACKUP"
fi

rm -rf "$DEST"
mkdir -p "$DEST"

rsync -a \
  --exclude='node_modules' \
  --exclude='.git' \
  --exclude='.expo' \
  --exclude='dist' \
  --exclude='package-lock.json' \
  --exclude='codemagic.yaml' \
  --exclude='README.md' \
  "$SOURCE/" "$DEST/"

if [[ -n "$EMBED_BACKUP" ]]; then
  cp "$EMBED_BACKUP" "$EMBED_FILE"
  rm -f "$EMBED_BACKUP"
fi

echo "Refreshed template under $DEST"
echo ""
echo "Next steps:"
echo "  cd $REPO_ROOT"
echo "  go test -tags=integration ./internal/controllers/ -run TestCreateAppProject_MobileAppNative"
echo "  git add internal/agent/templates/expo_native"
echo "  git commit -m 'chore(template): refresh vendored expo template to <upstream sha>'"

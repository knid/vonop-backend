#!/usr/bin/env bash
# Fail the build if any PostHog/Resend event call site uses a string literal
# instead of a constants.EventName. Every event name must be declared once
# in internal/constants/events.go so PostHog/Resend stay in sync with code.
#
# Detects:
#   - TrackUserEvent(user, "literal", ...)
#   - TrackSystemEvent(distinctID, "literal", ...)
#   - EventService.SendEvent(user, "literal", ...)
#   - posthog.Capture{Event: "literal", ...}
#
# Allows:
#   - constants.EVENT_X
#   - string(constants.EVENT_X)   (for posthog.Capture.Event which is string-typed)
#   - constants.EVENT_PREFIX_X + constants.EventName(suffix)   (dynamic suffix events)

set -euo pipefail

cd "$(dirname "$0")/.."

violations=$(grep -rn --include='*.go' \
    --exclude-dir='.claude' --exclude-dir='.worktrees' \
    -E '(TrackUserEvent|TrackSystemEvent|EventService\.SendEvent)\([^,]+,\s*"' . || true)

capture_violations=$(grep -rn --include='*.go' \
    --exclude-dir='.claude' --exclude-dir='.worktrees' \
    -E '(posthog\.Capture\{[^}]*)?Event:\s*"' . \
    | grep -v 'responses\.SSEEvent' \
    | grep -v 'SSEEvent{' \
    | grep -v '_test\.go' \
    | grep -v 'agent_run_handler\.go' \
    || true)

if [[ -n "$violations" || -n "$capture_violations" ]]; then
    echo "ERROR: PostHog/Resend event names must come from constants/events.go" >&2
    echo "" >&2
    [[ -n "$violations" ]] && echo "$violations" >&2
    [[ -n "$capture_violations" ]] && echo "$capture_violations" >&2
    echo "" >&2
    echo "Add a constant to internal/constants/events.go and reference it instead." >&2
    exit 1
fi

echo "OK: every event name comes from constants/events.go"

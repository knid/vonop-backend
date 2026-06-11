#!/bin/sh
set -eu

if [ -z "${DATABASE_URI:-}" ]; then
  echo "ERROR: DATABASE_URI is required for the production LangGraph Agent Server" >&2
  exit 1
fi

if [ -z "${REDIS_URI:-}" ]; then
  echo "ERROR: REDIS_URI is required for the production LangGraph Agent Server" >&2
  exit 1
fi

LANGGRAPH_SERVER_PORT="${LANGGRAPH_SERVER_PORT:-2024}"
export LANGGRAPH_SERVER_PORT

/storage/entrypoint.sh &
langgraph_pid=$!

uvicorn wemob_agent.internal_api:app --host 0.0.0.0 --port 2025 &
internal_api_pid=$!

term() {
  kill "$langgraph_pid" "$internal_api_pid" 2>/dev/null || true
}
trap term INT TERM

while :; do
  if ! kill -0 "$langgraph_pid" 2>/dev/null; then
    wait "$langgraph_pid" || exit $?
    exit 1
  fi
  if ! kill -0 "$internal_api_pid" 2>/dev/null; then
    wait "$internal_api_pid" || exit $?
    exit 1
  fi
  sleep 1
done

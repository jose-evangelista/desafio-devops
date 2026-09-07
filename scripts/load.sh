#!/usr/bin/env bash
#
# Load generator for the demo. Without traffic the dashboard renders empty, and
# an empty dashboard looks like a broken one.
#
#   ./load.sh
#   URL=http://10.0.0.10 DURATION=120 INTERVAL=0.1 NOISE=10 ./load.sh
#
set -euo pipefail

URL="${URL:-http://localhost}"
DURATION="${DURATION:-300}"
INTERVAL="${INTERVAL:-0.2}"
NOISE="${NOISE:-5}"

command -v curl >/dev/null || { echo "curl not found" >&2; exit 1; }

deadline=$(( $(date +%s) + DURATION ))
ok=0
noise=0

cleanup() {
  echo
  echo "sent: ${ok} to /projeto-korp, ${noise} to unknown paths"
}
trap cleanup EXIT INT TERM

echo "load against ${URL} for ${DURATION}s (interval ${INTERVAL}s, noise ${NOISE}%)"

while [ "$(date +%s)" -lt "$deadline" ]; do
  if [ "$NOISE" -gt 0 ] && [ $(( RANDOM % 100 )) -lt "$NOISE" ]; then
    curl -s -o /dev/null "${URL}/does-not-exist-$(( RANDOM % 5 ))" || true
    noise=$(( noise + 1 ))
  else
    curl -s -o /dev/null "${URL}/projeto-korp" || true
    ok=$(( ok + 1 ))
  fi
  sleep "$INTERVAL"
done

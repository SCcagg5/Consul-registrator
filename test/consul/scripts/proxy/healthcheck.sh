#!/usr/bin/env sh
set -e

READY_PORT=19100

if ! wget -q -O /dev/null "http://127.0.0.1:${READY_PORT}/ready"; then
  echo "Envoy not ready on /ready (port ${READY_PORT})"
  exit 2
fi

if ! pgrep -f "envoy" >/dev/null 2>&1; then
  echo "Envoy process not running"
  exit 3
fi

echo "OK"
exit 0

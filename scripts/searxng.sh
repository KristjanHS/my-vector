#!/usr/bin/env bash
# SearXNG on 127.0.0.1:8888 for the search provider (web-search Stage 2.3). Manual only: no restart policy.
#
#   scripts/searxng.sh up | down | status | check     (check exits non-zero unless a JSON search returns results)
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
IMAGE=searxng/searxng:2026.11.2-cccbc9e15
NAME=my-vector-searxng
PORT=${SEARXNG_PORT:-8888}
WORK=$ROOT/robot/work/searxng

case ${1:-} in
up)
  mkdir -p "$WORK"
  [[ -s $WORK/secret ]] || { umask 077; openssl rand -hex 32 >"$WORK/secret"; }
  sed "s/@SECRET@/$(cat "$WORK/secret")/" "$ROOT/scripts/searxng/settings.yml" >"$WORK/settings.yml"
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker run -d --name "$NAME" -e FORCE_OWNERSHIP=false -p "127.0.0.1:$PORT:8080" -v "$WORK:/etc/searxng" "$IMAGE" >/dev/null
  for _ in $(seq 30); do
    if curl -sf -o /dev/null "http://127.0.0.1:$PORT/healthz"; then echo "searxng up on 127.0.0.1:$PORT"; exit 0; fi
    sleep 1
  done
  echo "searxng did not answer in 30 s; docker logs $NAME" >&2; exit 1 ;;
down) docker rm -f "$NAME" >/dev/null && echo "searxng down" ;;
check) curl -sf "http://127.0.0.1:$PORT/search?q=tartu&format=json" | jq -e '.results|length>0' >/dev/null && echo "searxng ok" ;;
status) docker ps --filter "name=^$NAME\$" --format '{{.Status}}  {{.Image}}' ;;
*) sed -n 2,4p "$0" >&2; exit 2 ;;
esac

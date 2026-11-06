#!/usr/bin/env bash
# Live check of a running service: smoke.sh [host:port] (default 127.0.0.1:8091). Per voice: HTTP 200, non-empty
# even-length PCM, first-audio + total time. Exit 1 on any failure.
set -euo pipefail
url=http://${1:-127.0.0.1:8091}
text="Hello, I'm Vector. It's twelve degrees and a little cloudy. Shall I set a timer?"
text_et="Tere, mina olen Vector. Kell on 14.05 ja täna on 6. november."
out=$(mktemp)
trap 'rm -f "$out"' EXIT
curl -fsS --max-time 5 "$url/health"
echo
fail=0
for voice in cori af_bella mari; do
  t=$text
  [[ $voice == mari ]] && t=$text_et
  w=$(curl -sS --max-time 60 -o "$out" -w '%{http_code} %{time_starttransfer} %{time_total}' \
    -H 'Content-Type: application/json' -d "{\"text\": \"$t\", \"voice\": \"$voice\"}" "$url/tts") || w="000 0 0"
  read -r code first total <<<"$w"
  bytes=$(stat -c %s "$out")
  if [[ $code == 200 ]] && ((bytes > 0 && bytes % 2 == 0)); then
    printf '%-9s OK   first audio %5.2fs  total %5.2fs  audio %5.2fs\n' "$voice" "$first" "$total" "$(bc -l <<<"$bytes / 32000")"
  else
    printf '%-9s FAIL http %s, %s bytes\n' "$voice" "$code" "$bytes"
    fail=1
  fi
done
exit "$fail"

#!/usr/bin/env bash
# One-shot, delete after use: move wire-pod's private state from chipper/ and certs/ into secrets/server/ (vars/fork_paths.go), pin HEAD, restart.
# Usage: scripts/migrate-secrets.sh   then restart myvector-tts/-stt (they read secrets/server/myvectorConfig.json). Refuses a clash or a foreground wire-pod.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
git -C "$repo" cat-file -e HEAD:chipper/pkg/vars/fork_paths.go 2>/dev/null || { echo "FAIL: HEAD has no fork_paths.go; commit first" >&2; exit 1; }
svc=0; systemctl is-active -q wire-pod && svc=1 # restarted right after the move: the old build writes chipper/ until it stops
((svc)) || [[ -z $(ss -Hltn 'sport = :443') ]] || { echo "FAIL: a foreground wire-pod holds :443; stop it first" >&2; exit 1; }
umask 077
dst=$repo/secrets/server
mkdir -p "$dst/jdocs" "$dst/session-certs" && chmod 700 "$repo/secrets" "$dst"
mv_in() { # mv_in <src, repo-relative> <dst, secrets/server-relative>
  local s=$repo/$1 d=$dst/$2
  [[ -e $s ]] || return 0
  [[ ! -d $d ]] || rmdir "$d" 2>/dev/null || true # wire-pod's fork_paths.go init may have made it empty
  [[ ! -e $d ]] || { echo "FAIL: both $1 and secrets/server/$2 exist; merge by hand" >&2; exit 1; }
  mv "$s" "$d" && echo "moved $1 -> secrets/server/$2"
}
shopt -s nullglob
for f in "$repo"/chipper/{apiConfig.json,botConfig.json,myvectorConfig.json,myvectorPrompt.json,myvectorSearch.json*,myvectorMemory.json*,myvectorMemory}; do
  mv_in "chipper/${f##*/}" "${f##*/}"
done
for f in "$repo"/chipper/jdocs/*.json; do mv_in "chipper/jdocs/${f##*/}" "jdocs/${f##*/}"; done
for f in "$repo"/chipper/session-certs/*; do [[ ${f##*/} == placeholder ]] || mv_in "chipper/session-certs/${f##*/}" "session-certs/${f##*/}"; done
mv_in certs certs
b=$repo/secrets/server-state/apiConfig.json.bak
if [[ -e $repo/chipper/apiConfig.json.bak && ! -e $b ]]; then mkdir -p "${b%/*}" && mv "$repo/chipper/apiConfig.json.bak" "$b" && echo "moved chipper/apiConfig.json.bak -> secrets/server-state/"; fi
chmod -R go-rwx "$dst"
"$repo/scripts/pin-server.sh"
if ((svc)); then "$repo/scripts/restart-service.sh"; else echo "wire-pod was not running: make start"; fi
echo "OK: now restart myvector-tts and myvector-stt (sudo systemctl restart myvector-tts myvector-stt)"

#!/usr/bin/env bash
# wire-pod.service ExecStart (root): build the pinned commit's server from a clean git archive copy, then run it inside
# chipper/, so apiConfig.json, jdocs/, session-certs/, ../certs etc. resolve exactly as with start.sh.
# The pin is robot/work/server-sha (a full sha): make ship writes it before its restart, make daemon-enable seeds it with HEAD.
# Uncommitted edits never run here. Gap: chipper/webroot is still read from the working tree at runtime.
# The working tree is still runnable by hand: with the service disabled, make start runs chipper/start.sh (go run).
# Builds are cached per sha in robot/work/server-build/<sha>/chipper (root-owned; the newest 3 are kept). The running binary's
# path names its sha: ship.sh reads it from the service's main pid.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
work=$repo/robot/work
die() { echo "run-server.sh: $*" >&2; exit 1; }
pin=$(cat "$work/server-sha" 2>/dev/null) || die "no $work/server-sha: make ship writes it, make daemon-enable seeds it"
sha=$(git -C "$repo" rev-parse --verify "$pin^{commit}") || die "server-sha $pin is not a commit"

# The firewall of the same commit (a nft error stops the service, as ExecStartPre did).
git -C "$repo" show "$sha:scripts/wire-pod-guard.nft" | /usr/sbin/nft -f - || die "firewall from $sha did not load"

cd "$repo/chipper"
[[ -f source.sh ]] || die "no chipper/source.sh: run setup.sh"
set +u
# shellcheck source=/dev/null
. ./source.sh
set -u
[[ ${STT_SERVICE:-} == vosk ]] || die "builds the vosk server only (STT_SERVICE=${STT_SERVICE:-unset}); use chipper/start.sh"
export CGO_ENABLED=1 CGO_CFLAGS="-I/root/.vosk/libvosk" CGO_LDFLAGS="-L /root/.vosk/libvosk -lvosk -ldl -lpthread"
export LD_LIBRARY_PATH="/root/.vosk/libvosk:${LD_LIBRARY_PATH:-}"

builds=$work/server-build
bin=$builds/$sha/chipper
if [[ ! -x $bin ]]; then
  mkdir -p "$builds"
  tmp=$(mktemp -d "$builds/.tmp.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT
  git -C "$repo" archive "$sha" chipper | tar -x -C "$tmp"
  tags=nolibopusfile
  [[ ${USE_INBUILT_BLE:-} == true ]] && tags+=,inbuiltble
  short=$(git -C "$repo" rev-parse --short "$sha")
  echo "building server $short from a clean copy"
  (cd "$tmp/chipper" && /usr/local/go/bin/go build -tags "$tags" \
    -ldflags "-X 'github.com/kercre123/wire-pod/chipper/pkg/vars.CommitSHA=$short'" -o "$tmp/chipper.bin" cmd/vosk/main.go)
  mkdir -p "$builds/$sha"
  mv "$tmp/chipper.bin" "$bin"
  rm -rf "$tmp"
  trap - EXIT
  # Keep the newest 3 builds (this one included).
  find "$builds" -mindepth 1 -maxdepth 1 -type d ! -name '.tmp.*' -printf '%T@ %p\n' | sort -rn | tail -n +4 | cut -d' ' -f2- |
    xargs -r rm -rf --
fi
exec "$bin"

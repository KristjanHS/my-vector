#!/usr/bin/env bash
# wire-pod.service ExecStart (as you; its ExecStartPre loads the firewall as root): build the commit in robot/work/server-sha from a git archive copy into
# robot/work/server-build/<sha>/chipper (ship.sh parses that path), then exec it with cwd chipper/ as start.sh does.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
work=$repo/robot/work
die() { echo "run-server.sh: $*" >&2; exit 1; }
pin=$(cat "$work/server-sha" 2>/dev/null) || die "no $work/server-sha: make ship / make server-pin write it, make daemon-enable seeds it"
sha=$(git -C "$repo" rev-parse --verify "$pin^{commit}") || die "server-sha $pin is not a commit"

# A unit installed before the switch to User= still runs this as root without the ExecStartPre: load the firewall here.
if ((EUID == 0)); then "$repo/scripts/load-guard.sh" || die "firewall from $sha did not load"; fi

cd "$repo/chipper"
[[ -f source.sh ]] || die "no chipper/source.sh: run setup.sh"
set +u
# shellcheck source=/dev/null
. ./source.sh
set -u
[[ ${STT_SERVICE:-} == vosk ]] || die "builds the vosk server only (STT_SERVICE=${STT_SERVICE:-unset}); use chipper/start.sh"
vosk=$HOME/.vosk/libvosk # make daemon-enable copies it from /root/.vosk (where setup.sh puts it)
[[ -d $vosk ]] || die "no $vosk: make daemon-enable"
export CGO_ENABLED=1 CGO_CFLAGS="-I$vosk" CGO_LDFLAGS="-L $vosk -lvosk -ldl -lpthread"
export LD_LIBRARY_PATH="$vosk:${LD_LIBRARY_PATH:-}"

builds=$work/server-build
bin=$builds/$sha/chipper
if [[ ! -x $bin ]]; then
  # Fixed dir: Go's build cache keys on the package dir, a new path per build recompiles everything.
  src=$builds/.src
  rm -rf "$src"
  mkdir -p "$src"
  git -C "$repo" archive "$sha" chipper | tar -x -C "$src"
  tags=nolibopusfile
  [[ ${USE_INBUILT_BLE:-} == true ]] && tags+=,inbuiltble
  short=$(git -C "$repo" rev-parse --short "$sha")
  echo "building server $short from a clean copy"
  # -buildvcs=false: never let go run git on the main checkout.
  (cd "$src/chipper" && /usr/local/go/bin/go build -buildvcs=false -tags "$tags" \
    -ldflags "-X 'github.com/kercre123/wire-pod/chipper/pkg/vars.CommitSHA=$short'" -o "$src/chipper.bin" cmd/vosk/main.go)
  mkdir -p "$builds/$sha"
  mv "$src/chipper.bin" "$bin"
  # Keep the newest 3 builds; a prune failure never stops the server.
  find "$builds" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -printf '%T@\t%p\0' | sort -zrn | tail -zn +4 | cut -zf2- |
    xargs -0r rm -rf -- || true
fi
exec "$bin"

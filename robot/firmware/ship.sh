#!/usr/bin/env bash
# Ship the patched firmware: tests, patches, robot compat + myvector-stt freshness checks, build (live progress), built-asset check, wire-pod restart
# (only if chipper/ or the firewall changed since the running server's sha), deploy, robot settings, robot smoke check, git push.
# Usage: ship.sh [--no-deploy]   --no-deploy stops after the build (no server restart, no deploy, no push).
#   Runs only in the main checkout on main (make syncwt first), never in a worktree.
# HEAD-only: checks, patches and apply.sh come from the committed HEAD; uncommitted fork files are ignored.
#   The server is the exception: wire-pod.service runs `go run` from the working tree, so a needed restart
#   requires HEAD's chipper/ + firewall = the shipped sha's and no uncommitted ones (else FAIL before the build).
# Log: every run is recorded to robot/work/ship-logs/ship-<time>-<sha>.log (latest.log = newest); the newest 20 are kept.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
src=$repo/robot/work/wire-os-victor
warns='' # compat.sh WARN + settings.sh WARN/SETUP lines, repeated at the end of the run
add_warns() { local w; w=$(grep '^WARN\|^SETUP' <<<"$1" || true); [[ -z $w ]] || warns+=${warns:+$'\n'}$w; }
show_warns() { [[ -z $warns ]] || printf '\033[1;33m%s\033[0m\n' "$warns" >&2; }
die() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; show_warns; [[ -z ${SHIP_LOG:-} ]] || echo "log: $SHIP_LOG" >&2; exit 1; }
case ${1:-} in '') deploy=1 ;; --no-deploy) deploy=0 ;; *) die "unknown arg: $1 (only --no-deploy)" ;; esac
total=$((5 + deploy * 6)); i=0
step() { i=$((i + 1)); printf '\n\033[1;36m==> [%s/%s] %s\033[0m\n' "$i" "$total" "$*"; }
[[ $(git -C "$repo" rev-parse --absolute-git-dir) == "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)" ]] ||
  die "run from the main checkout, not a worktree: make syncwt, then make -C ~/projects/my-vector ship"
[[ $(git -C "$repo" branch --show-current) == main ]] || die "main checkout is not on main"
full=${SHIP_FULL:-$(git -C "$repo" rev-parse HEAD)}; sha=$(git -C "$repo" rev-parse --short "$full")

# Re-run this script under `script`, which records exactly what the terminal shows (live progress included)
# and passes the exit code through (-e). Prune first so the new log makes the newest $keep.
logs=$repo/robot/work/ship-logs keep=20
if [[ -z ${SHIP_LOG:-} ]]; then
  mkdir -p "$logs"
  find "$logs" -maxdepth 1 -name 'ship-*.log' -print0 | sort -rz | tail -zn +"$keep" | xargs -0r rm -f --
  export SHIP_FULL=$full  # the re-run ships the sha named in the log, even if HEAD moves in between
  export SHIP_LOG=$logs/ship-$(date +%Y%m%d-%H%M%S)-$sha.log
  ln -sfn "$(basename "$SHIP_LOG")" "$logs/latest.log"
  exec script -qefc "$(printf '%q ' "$0" "$@")" "$SHIP_LOG"
fi
echo "log: $SHIP_LOG"

dirty=$(git -C "$repo" status --porcelain | wc -l)
head=$(mktemp -d "$repo/robot/work/ship-head.XXXXXX")
trap 'rm -rf "$head"' EXIT
# fork_smoke.go + myvector/voice too: check-static.sh S5 reads them repo-relative and fails if smoke.sh is there without them.
# robot/env.sh: settings.sh + smoke.sh source it from $pd/.. ; ls-tree drops it (and voice/) for an older sha, which has none.
# shellcheck disable=SC2046 # unquoted on purpose: empty for an older sha
git -C "$repo" archive "$full" robot/firmware chipper/pkg/wirepod/sdkapp/fork_smoke.go \
  $(git -C "$repo" ls-tree --name-only "$full" robot/env.sh robot/diskguard chipper/pkg/myvector/voice) |
  tar -x -C "$head"
pd=$head/robot/firmware
if ((deploy)); then
  envsh=$head/robot/env.sh; [[ -f $envsh ]] || envsh=$here/../env.sh
  # shellcheck source=robot/env.sh
  . "$envsh"
  write_clone_ip "$src"
fi

# The source tree must be the tip apply.sh built: pin + exactly n commits, no hunks, staged edits or untracked files.
tree_is_patches() {
  local d; d=$(git -C "$src" status --porcelain --untracked-files=normal --ignore-submodules=none)
  [[ $(git -C "$src" rev-parse HEAD) == "$tip" && $(git -C "$src" rev-list --count "$pin..$tip") == "$n" && -z $d ]] ||
    die "tree differs from pin + $n series patches (another session in flight?): $d"
}

step "check HEAD $sha ($dirty uncommitted files ignored): drift, shell syntax, Go tests"
"$repo/scripts/check.sh" --head "$full" || die "make check (HEAD)"

step "patches: apply to $src, tree = pin + series"
VICTOR_SRC=$src "$pd/apply.sh"
tip=$(git -C "$src" rev-parse HEAD)
pin=$(sed -n 's/^pin=//p' "$pd/apply.sh")
n=$(grep -vc '^[[:space:]]*$' "$pd/series") || die "empty series"
tree_is_patches
want=$(git -C "$src" diff --name-only "$pin" "$tip")
echo "tree = pin + $n patches ($(echo "$want" | wc -l) files)"

if ((deploy)); then
  step "compat: tree VICTOR_COMPAT_VERSION = robot /etc/victor-compat-version"
  cs=$pd/compat.sh; [[ -f $cs ]] || cs=$here/compat.sh # a pre-R7 sha has none
  out=$("$cs" "$src" "$tip" 2>&1) || die "${out#FAIL: }"
  echo "$out"
  add_warns "$out"

  # Server inputs: the Go tree, plus the firewall that ExecStartPre loads on each (re)start.
  srv=(chipper scripts/wire-pod-guard.nft)
  # start.sh stamps `git rev-parse --short HEAD` into the `go run` child of the service's main pid; empty = no server.
  server_sha() {
    local pid; pid=$(systemctl show -p MainPID --value wire-pod 2>/dev/null || true)
    [[ -n $pid && $pid != 0 ]] || return 0
    pgrep -a -P "$pid" | grep -o 'vars\.CommitSHA=[0-9a-f]*' | head -1 | cut -d= -f2 || true
  }
  # Sets running + restart. An unknown sha (git diff exits 128) counts as changed.
  server_check() {
    running=$(server_sha); restart=0
    [[ -n $running ]] && git -C "$repo" diff --quiet "$running" "$full" -- "${srv[@]}" 2>/dev/null || restart=1
    if ((restart)); then
      systemctl is-enabled -q wire-pod ||
        die "server inputs changed since the running server (${running:-none}) and wire-pod.service is not enabled: make daemon-enable"
      server_tree_ok
    fi
  }
  # The restart runs the working tree: its server inputs must be exactly the shipped sha's (HEAD may move elsewhere).
  server_tree_ok() {
    git -C "$repo" diff --quiet HEAD "$full" -- "${srv[@]}" || die "HEAD's ${srv[*]} differ from $sha: a wire-pod restart would run other server code"
    local d; d=$(git -C "$repo" status --porcelain --untracked-files=normal -- "${srv[@]}")
    [[ -z $d ]] || die "uncommitted server changes, which a wire-pod restart would run live: $d"
  }
  server_check
  # The unit file is a template that only make daemon-enable installs; a restart doesn't apply it.
  [[ -z $running ]] || git -C "$repo" diff --quiet "$running" "$full" -- scripts/wire-pod.service 2>/dev/null ||
    add_warns "WARN scripts/wire-pod.service changed since $running: run make daemon-enable to install it"
  # myvector-stt runs the tree's et-stt/server.py and has no NOPASSWD restart rule, so it is checked, not restarted:
  # one started before the shipped server.py may not serve wire-pod's /stt?lang= (an old one 404s all STT). Inactive -> skip.
  if systemctl is-active -q myvector-stt 2>/dev/null; then
    stt_up=$(date -d "$(systemctl show -p ActiveEnterTimestamp --value myvector-stt)" +%s 2>/dev/null || echo 0)
    stt_ct=$(git -C "$repo" log -1 --format=%ct "$full" -- et-stt/server.py)
    ((${stt_ct:-0} <= stt_up)) || die "myvector-stt started before $sha's last et-stt/server.py commit — run: make stt-enable"
    echo "myvector-stt started after $sha's last et-stt/server.py commit"
  fi
fi

json=$(echo "$want" | grep '\.json$' || true)
step "patched JSON parses"
for f in $json; do
  python3 -I -c 'import json,re,sys; json.loads(re.sub(r"(?<![:\"])//[^\n]*", "", open(sys.argv[1]).read()))' "$src/$f" ||
    die "bad JSON: $f"
done
echo "$(echo "$json" | grep -c . || true) JSON files OK"

step "build: ./build/build-v.sh"
(cd "$src" && script -qec ./build/build-v.sh /dev/null) || die "build"

step "built = patched (bin markers, built JSON); tree unchanged during the build"
VICTOR_SRC=$src "$pd/check-static.sh" --built || die "built firmware lacks a patch (check-static.sh --built)"
tree_is_patches
echo "built firmware matches the tree"

((deploy)) || { echo; echo "built, not deployed (--no-deploy) at $(TZ=Europe/Tallinn date "+%Y-%m-%d %H:%M %Z"). log: $SHIP_LOG"; exit 0; }

# Before the deploy: a server that won't start stops the ship with the robot untouched, and the robot
# reboots straight onto the new server; smoke.sh's wire-pod link check then covers both.
server_check # again: the server may have been restarted during the build
if ((restart)); then
  step "server: ${srv[*]} changed since ${running:-none (not running)} -> restart wire-pod on $sha"
  "$repo/scripts/restart-service.sh" ||
    die "wire-pod not up on :443 within the wait (journal above; it may still be compiling) — robot untouched, not pushed; fix and ship again"
  server_tree_ok # again: go run compiled from the tree while we waited
  now=$(server_sha)
  # shellcheck disable=SC2015 # die on either failure is the intent
  [[ -n $now ]] && git -C "$repo" diff --quiet "$now" "$full" -- "${srv[@]}" 2>/dev/null ||
    die "wire-pod restarted but reports ${now:-no sha}, whose server code is not $sha's — robot untouched, not pushed"
  echo "wire-pod runs $now (server code = $sha)"
else
  step "server: wire-pod already runs $running, ${srv[*]} unchanged — no restart"
fi

step "deploy: ./build/deploy-v.sh to $ip"
(cd "$src" && script -qec ./build/deploy-v.sh /dev/null) || die "deploy"

# A settings failure warns, never stops the ship: the robot already runs $sha.
step "robot settings: settings.sh (master_volume, no-auto-update, T2 voice, /data guard)"
out=$("$pd/settings.sh" 2>&1) || true
echo "$out"
add_warns "$out"

step "robot smoke test: smoke.sh (units up + stable, log, head, face, speech, base still)"
"$pd/smoke.sh" || die "robot smoke test failed — the robot is ALREADY running $sha, NOT pushed; fix and ship again, or ship the previous sha"

# Push the pinned sha, not the branch tip: a parallel session may have committed during the build,
# and pushing the tip would publish commits the robot is not running. Keeps origin == robot.
step "push: $sha to origin main"
git -C "$repo" push origin "$full:refs/heads/main" ||
  die "git push failed — the robot is ALREADY running $sha; push it by hand so origin == robot (rejected? origin may already have it, git fetch to check)"
echo
echo "shipped $sha at $(TZ=Europe/Tallinn date "+%Y-%m-%d %H:%M %Z") (log: $SHIP_LOG)"
show_warns

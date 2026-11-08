#!/usr/bin/env bash
# Ship the patched firmware: pod-trust interlock (T6), tests, patches, robot keys (hostkey + sshkey apply.sh), robot compat + myvector-stt freshness checks, build (live progress), built-asset check, wire-pod restart
# (only if chipper/, the firewall or run-server.sh changed since the running server's sha), deploy, robot settings, robot smoke check, git push.
# Usage: ship.sh [--no-deploy]   --no-deploy stops after the build (no server restart, no deploy, no push).
#   Runs only in the main checkout on main (make syncwt first), never in a worktree.
# HEAD-only: checks, patches and apply.sh come from the committed HEAD; uncommitted fork files are ignored.
#   The server too: the restart pins the shipped sha (scripts/pin-server.sh), which run-server.sh builds; only the tree's
#   launchers and runtime data ($data) must equal the shipped sha's.
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
total=$((5 + deploy * 8)); i=0
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
# robot/env.sh: settings.sh + smoke.sh source it from $pd/.. (robot/lib: the installers' etc.sh); ls-tree drops what an older sha lacks.
# shellcheck disable=SC2046 # unquoted on purpose: empty for an older sha
git -C "$repo" archive "$full" robot/firmware chipper/pkg/wirepod/sdkapp/fork_smoke.go \
  $(git -C "$repo" ls-tree --name-only "$full" robot/env.sh robot/lib robot/diskguard robot/sshkey robot/hostkey robot/epod robot/netguard chipper/pkg/myvector/voice) |
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

if ((deploy)); then
  # T6: a series with 0015 (vic-cloud drops the DDL root) ships only once wire-pod serves our CA's leaf; first, before any robot step.
  step "pod-trust interlock: robot/epod/interlock.sh on $sha's series"
  if [[ -f $head/robot/epod/interlock.sh ]]; then "$head/robot/epod/interlock.sh" "$pd/series" || die "pod-trust interlock (above)"
  else echo "no robot/epod/interlock.sh at $sha; skipped"; fi
fi

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
  # compat, deploy, settings and smoke all ssh with ROBOT_KEY, so the robot must accept it first (plan K6); hostkey first: after a reflash only it logs in (H1).
  step "robot keys: hostkey + sshkey apply.sh"
  for sk in hostkey sshkey; do
    if [[ -f $head/robot/$sk/apply.sh ]]; then out=$(bash "$head/robot/$sk/apply.sh" 2>&1) || die "robot keys: ${out//$'\n'/; }"; echo "$out"
    else echo "no robot/$sk at $sha; skipped"; fi
  done

  step "compat: tree VICTOR_COMPAT_VERSION = robot /etc/victor-compat-version"
  cs=$pd/compat.sh; [[ -f $cs ]] || cs=$here/compat.sh # a pre-R7 sha has none
  out=$("$cs" "$src" "$tip" 2>&1) || die "${out#FAIL: }"
  echo "$out"
  add_warns "$out"

  # Server inputs: the Go tree, the firewall and the launchers that build + run the pinned commit on each (re)start.
  srv=(chipper scripts/wire-pod-guard.nft scripts/run-server.sh scripts/load-guard.sh)
  # Tracked data the server still reads from the tree at runtime (cwd chipper/), not from the pinned build.
  data=(chipper/intent-data chipper/webroot chipper/weather-map.json chipper/stttest.pcm chipper/epod)
  # run-server.sh execs robot/work/server-build/<full sha>/chipper as the service's main pid; empty = no server (or still building).
  server_sha() {
    local pid; pid=$(systemctl show -p MainPID --value wire-pod 2>/dev/null || true)
    [[ -n $pid && $pid != 0 ]] || return 0
    ps -o args= -p "$pid" | grep -o 'server-build/[0-9a-f]*/chipper' | cut -d/ -f2 || true
  }
  # Sets running + restart. An unknown sha (git diff exits 128) counts as changed.
  server_check() {
    running=$(server_sha); restart=0
    [[ -n $running ]] && git -C "$repo" diff --quiet "$running" "$full" -- "${srv[@]}" 2>/dev/null || restart=1
    if ((restart)); then
      systemctl is-enabled -q wire-pod ||
        die "server inputs changed since the running server (${running:-none}) and wire-pod.service is not enabled: make daemon-enable"
      server_unit_ok
    fi
  }
  # The restart must build the pin, not run the tree: the installed unit uses run-server.sh, the tree's launchers
  # (which systemd runs) and runtime data are the shipped sha's. Uncommitted Go code is ignored.
  server_unit_ok() {
    grep -q 'ExecStart=.*/scripts/run-server\.sh' /etc/systemd/system/wire-pod.service 2>/dev/null ||
      die "installed wire-pod.service does not run scripts/run-server.sh (a restart would go run the working tree): make daemon-enable"
    git -C "$repo" diff --quiet "$full" -- scripts/run-server.sh scripts/load-guard.sh ||
      die "the tree's scripts/run-server.sh or load-guard.sh differs from $sha's, and systemd runs the tree's copy: commit it or restore it"
    local d; d=$(git -C "$repo" diff --name-only "$full" -- "${data[@]}"; git -C "$repo" status --porcelain --untracked-files=normal -- "${data[@]}")
    [[ -z $d ]] || die "server data the restarted wire-pod reads from the tree differs from $sha's: $d"
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
  step "server: ${srv[*]} changed since ${running:-none (not running)} -> pin $sha, restart wire-pod"
  "$repo/scripts/pin-server.sh" "$full"
  if ! "$repo/scripts/restart-service.sh"; then
    # Don't leave a pin that may not build: every crash-restart and boot would retry it with Vector offline.
    if [[ -n $running ]]; then { "$repo/scripts/pin-server.sh" "$running" && "$repo/scripts/restart-service.sh"; } || true; fi
    die "wire-pod not up on :443 within the wait (journal above; maybe still building $sha) — ${running:+pin rolled back to $running, }robot untouched, not pushed; fix and ship again"
  fi
  now=$(server_sha)
  [[ $now == "$full" ]] ||
    die "wire-pod restarted but runs ${now:-no pinned build}, not $sha — robot untouched, not pushed"
  echo "wire-pod runs $now (server code = $sha)"
else
  step "server: wire-pod already runs $running, ${srv[*]} unchanged — no restart"
fi

step "deploy: ./build/deploy-v.sh to $ip"
# Patch 0014: deploy-v.sh ssh-adds MYVECTOR_DEPLOY_KEY (gitignored in the clone) instead of the image's robot_sshkey.
trap 'rm -rf "$head"; rm -f "$src/myvector_sshkey"' EXIT
install -m 600 "$key" "$src/myvector_sshkey"
(cd "$src" && MYVECTOR_DEPLOY_KEY=myvector_sshkey script -qec ./build/deploy-v.sh /dev/null) || die "deploy"
rm -f "$src/myvector_sshkey"

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

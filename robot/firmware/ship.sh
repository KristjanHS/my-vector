#!/usr/bin/env bash
# Ship the patched firmware: tests, patches, build (live progress), built-asset check, deploy, robot settings, robot smoke check, git push.
# Usage: ship.sh [--no-deploy]   --no-deploy stops after the build (no deploy, no push).
#   Runs only in the main checkout on main (make syncwt first), never in a worktree.
# HEAD-only: checks, patches and apply.sh come from the committed HEAD; uncommitted fork files are ignored.
# Log: every run is recorded to robot/work/ship-logs/ship-<time>-<sha>.log (latest.log = newest); the newest 20 are kept.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
src=$repo/robot/work/wire-os-victor
warns='' # settings.sh WARN lines, repeated at the end of the run
show_warns() { [[ -z $warns ]] || printf '\033[1;33m%s\033[0m\n' "$warns" >&2; }
die() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; show_warns; [[ -z ${SHIP_LOG:-} ]] || echo "log: $SHIP_LOG" >&2; exit 1; }
case ${1:-} in '') deploy=1 ;; --no-deploy) deploy=0 ;; *) die "unknown arg: $1 (only --no-deploy)" ;; esac
total=$((5 + deploy * 4)); i=0
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
# fork_smoke.go too: check-static.sh S5 reads it repo-relative and fails if smoke.sh is there without it.
# robot/env.sh: settings.sh + smoke.sh source it from $pd/.. ; ls-tree drops it for a pre-R6 sha, which has none.
# shellcheck disable=SC2046 # unquoted on purpose: empty for a pre-R6 sha
git -C "$repo" archive "$full" robot/firmware chipper/pkg/wirepod/sdkapp/fork_smoke.go $(git -C "$repo" ls-tree --name-only "$full" robot/env.sh) |
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

step "deploy: ./build/deploy-v.sh to $ip"
(cd "$src" && script -qec ./build/deploy-v.sh /dev/null) || die "deploy"

# A settings failure warns, never stops the ship: the robot already runs $sha.
step "robot settings: settings.sh (master_volume, no-auto-update)"
out=$("$pd/settings.sh" 2>&1) || true
echo "$out"
warns=$(grep '^WARN\|^SETUP' <<<"$out" || true)

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

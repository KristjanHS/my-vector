#!/usr/bin/env bash
# Ship the patched firmware: tests, patches, build (live progress), built-asset check, deploy, robot smoke check.
# Usage: ship.sh [--no-deploy]   Runs only in the main checkout on main (make syncwt first), never in a worktree.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
src=$repo/robot/work/wire-os-victor
patches=(stay-on-charger.patch lift-lock.patch quiet-idle.patch)
deploy=1; [[ ${1:-} == --no-deploy ]] && deploy=0
total=$((5 + deploy * 2)); i=0
step() { i=$((i + 1)); printf '\n\033[1;36m==> [%s/%s] %s\033[0m\n' "$i" "$total" "$*"; }
die() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }
[[ $(git -C "$repo" rev-parse --absolute-git-dir) == "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)" ]] ||
  die "run from the main checkout, not a worktree: make syncwt, then make -C ~/projects/my-vector ship"
[[ $(git -C "$repo" branch --show-current) == main ]] || die "main checkout is not on main"

step "tests: upstream drift, shell syntax, chipper Go tests"
"$repo/scripts/upstream-drift.sh"
for f in "$repo"/robot/*/*.sh; do bash -n "$f" || die "syntax: $f"; done
echo "shell syntax OK"
(cd "$repo/chipper" && go test ./pkg/myvector/... 2>&1 | tail -20; exit "${PIPESTATUS[0]}") || die "go test"

step "patches: apply to $src, no edits outside them"
"$here/apply.sh"
want=$(for p in "${patches[@]}"; do sed -n 's#^diff --git a/\([^ ]*\) .*#\1#p' "$here/$p"; done | sort -u)
extra=$(comm -23 <(git -C "$src" diff --name-only | sort -u) <(echo "$want"))
[[ -z $extra ]] || die "edits outside the patches (another session in flight?): $extra"
echo "tree = exactly the ${#patches[@]} patches ($(echo "$want" | wc -l) files)"

json=$(echo "$want" | grep '\.json$' || true)
step "patched JSON parses"
for f in $json; do
  python3 -I -c 'import json,re,sys; json.loads(re.sub(r"(?<![:\"])//[^\n]*", "", open(sys.argv[1]).read()))' "$src/$f" ||
    die "bad JSON: $f"
done
echo "$(echo "$json" | grep -c . || true) JSON files OK"

step "build: ./build/build-v.sh"
(cd "$src" && script -qec ./build/build-v.sh /dev/null) || die "build"

step "built assets carry the patched config"
for f in $json; do
  cmp -s "$src/$f" "$src/_build/vicos/Release/data/assets/cozmo_$f" ||
    die "stale in _build: $f"
done
echo "built config matches the tree"

((deploy)) || { echo; echo "built, not deployed (--no-deploy)"; exit 0; }

step "deploy: ./build/deploy-v.sh to $(cat "$src/robot_ip.txt")"
(cd "$src" && script -qec ./build/deploy-v.sh /dev/null) || die "deploy"

step "robot: vic-engine, vic-anim, vic-robot active"
ssh_() { ssh -i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa \
  -o HostKeyAlgorithms=+ssh-rsa "root@$(cat "$src/robot_ip.txt")" "$@"; }
for _ in $(seq 36); do
  ssh_ systemctl is-active -q vic-engine vic-anim vic-robot 2>/dev/null && { echo "services up"; break; }
  sleep 5
done
ssh_ systemctl is-active -q vic-engine vic-anim vic-robot || die "services not active after 3 min"
echo
echo "shipped. Restart the soak: setsid nohup robot/stay-on-charger/soak.sh >robot/work/soak.out 2>&1 </dev/null &"

#!/usr/bin/env bash
# Ship the patched firmware: tests, patches, build (live progress), built-asset check, deploy, robot smoke check, git push.
# Usage: ship.sh [--no-deploy]   --no-deploy stops after the build (no deploy, no push).
#   Runs only in the main checkout on main (make syncwt first), never in a worktree.
# HEAD-only: checks, patches and apply.sh come from the committed HEAD; uncommitted fork files are ignored.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
src=$repo/robot/work/wire-os-victor
patches=(stay-on-charger.patch lift-lock.patch quiet-idle.patch)
units=(vic-engine vic-anim vic-robot)
die() { printf '\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }
case ${1:-} in '') deploy=1 ;; --no-deploy) deploy=0 ;; *) die "unknown arg: $1 (only --no-deploy)" ;; esac
total=$((5 + deploy * 3)); i=0
step() { i=$((i + 1)); printf '\n\033[1;36m==> [%s/%s] %s\033[0m\n' "$i" "$total" "$*"; }
[[ $(git -C "$repo" rev-parse --absolute-git-dir) == "$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir)" ]] ||
  die "run from the main checkout, not a worktree: make syncwt, then make -C ~/projects/my-vector ship"
[[ $(git -C "$repo" branch --show-current) == main ]] || die "main checkout is not on main"
full=$(git -C "$repo" rev-parse HEAD); sha=$(git -C "$repo" rev-parse --short "$full")
dirty=$(git -C "$repo" status --porcelain | wc -l)
head=$(mktemp -d "$repo/robot/work/ship-head.XXXXXX")
trap 'rm -rf "$head"' EXIT
git -C "$repo" archive "$full" robot/stay-on-charger | tar -x -C "$head"
pd=$head/robot/stay-on-charger
if ((deploy)); then
  [[ -s $src/robot_ip.txt && -f $src/robot_sshkey ]] || die "need $src/robot_ip.txt and robot_sshkey"
  ip=$(<"$src/robot_ip.txt")
fi

# The source tree must equal HEAD + exactly the patches: no extra hunks, staged edits or untracked files.
tree_is_patches() {
  local t; t=$(mktemp -u "$repo/robot/work/ship-index.XXXXXX")
  GIT_INDEX_FILE=$t git -C "$src" read-tree HEAD
  for p in "${patches[@]}"; do GIT_INDEX_FILE=$t git -C "$src" apply --cached "$pd/$p"; done
  local d u; d=$(GIT_INDEX_FILE=$t git -C "$src" diff --name-only); rm -f "$t"
  u=$(git -C "$src" ls-files --others --exclude-standard)
  [[ -z $d$u ]] || die "tree differs from HEAD + patches (another session in flight?): $d $u"
}

step "check HEAD $sha ($dirty uncommitted files ignored): drift, shell syntax, Go tests"
"$repo/scripts/check.sh" --head "$full" || die "make check (HEAD)"

step "patches: apply to $src, tree = HEAD + patches"
VICTOR_SRC=$src "$pd/apply.sh"
tree_is_patches
want=$(for p in "${patches[@]}"; do sed -n 's#^diff --git a/\([^ ]*\) .*#\1#p' "$pd/$p"; done | sort -u)
echo "tree = HEAD + ${#patches[@]} patches ($(echo "$want" | wc -l) files)"

json=$(echo "$want" | grep '\.json$' || true)
step "patched JSON parses"
for f in $json; do
  python3 -I -c 'import json,re,sys; json.loads(re.sub(r"(?<![:\"])//[^\n]*", "", open(sys.argv[1]).read()))' "$src/$f" ||
    die "bad JSON: $f"
done
echo "$(echo "$json" | grep -c . || true) JSON files OK"

step "build: ./build/build-v.sh"
(cd "$src" && script -qec ./build/build-v.sh /dev/null) || die "build"

step "built assets carry the patched config; tree unchanged during the build"
for f in $json; do
  cmp -s "$src/$f" "$src/_build/vicos/Release/data/assets/cozmo_$f" || die "stale in _build: $f"
done
tree_is_patches
echo "built config matches the tree"

((deploy)) || { echo; echo "built, not deployed (--no-deploy)"; exit 0; }

step "deploy: ./build/deploy-v.sh to $ip"
(cd "$src" && script -qec ./build/deploy-v.sh /dev/null) || die "deploy"

step "robot: ${units[*]} active, still active 15 s later"
ssh_() { ssh -i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa \
  -o HostKeyAlgorithms=+ssh-rsa "root@$ip" "$@"; }
all_up() { ssh_ "for u in ${units[*]}; do systemctl is-active -q \$u || exit 1; done" 2>/dev/null; }
end=$((SECONDS + 180))
until all_up; do ((SECONDS < end)) || die "services not all active within 3 min"; sleep 5; done
sleep 15; all_up || die "a service dropped within 15 s (crash loop?)"
echo "services up"

# Push the pinned sha, not the branch tip: a parallel session may have committed during the build,
# and pushing the tip would publish commits the robot is not running. Keeps origin == robot.
step "push: $sha to origin main"
git -C "$repo" push origin "$full:refs/heads/main" ||
  die "git push failed — the robot is ALREADY running $sha; push it by hand so origin == robot (rejected? origin may already have it, git fetch to check)"
echo
echo "shipped $sha. Restart the soak: setsid nohup $here/soak.sh >$repo/robot/work/soak.out 2>&1 </dev/null &"

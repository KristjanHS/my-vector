#!/usr/bin/env bash
# Fails when the fork touches an upstream wire-pod path that is not listed below.
# Fork code goes in chipper/pkg/myvector/; glue into an upstream package is a fork_*.go file.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Upstream files the fork edits on purpose: the only places an upstream merge can conflict.
allowed_edits=(
  chipper/pkg/wirepod/sdkapp/bcassume.go # busy-spin fix while behavior control is held
  chipper/pkg/wirepod/sdkapp/server.go   # InitAutoHold() call
)

ref=${UPSTREAM_REF:-upstream/main}
git rev-parse -q --verify "$ref^{commit}" >/dev/null || { echo "missing $ref: git fetch upstream" >&2; exit 2; }
base=$(git merge-base HEAD "$ref")
roots=$(git ls-tree --name-only "$base")

bad=0
# Working tree vs the merge-base, so uncommitted edits count too.
while IFS=$'\t' read -r status path rest; do
  [[ $status == R* ]] && path=$rest # renames: check the new path
  top=${path%%/*}
  grep -qxF "$top" <<<"$roots" || continue # fork-only top level (docs/, .claude/, scripts/, ...)
  case $status in
    A* | R*)
      [[ $path == chipper/pkg/myvector/* || $(basename "$path") == fork_*.go ]] && continue
      echo "new fork file inside the upstream tree: $path (move it to chipper/pkg/myvector/ or name it fork_*.go)" ;;
    *)
      printf '%s\n' "${allowed_edits[@]}" | grep -qxF "$path" && continue
      echo "unlisted upstream edit ($status): $path (add it to allowed_edits or revert it)" ;;
  esac
  bad=1
done < <(git diff --name-status -M "$base")

if ((bad)); then exit 1; fi
echo "upstream drift OK: ${#allowed_edits[@]} allowed edits vs $(git rev-parse --short "$base")"

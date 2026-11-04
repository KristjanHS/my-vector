#!/usr/bin/env bash
# Static firmware checks (make check): series <-> *.patch <-> markers.tsv, smoke never drives the wheels,
# patches re-export byte-identical, each src marker is patch-specific. --built adds bin markers in _build.
# Usage: check-static.sh [--built]   (env: VICTOR_SRC; default <main checkout>/robot/work/wire-os-victor)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
die() { echo "FAIL: $*" >&2; exit 1; }
built=0
case ${1:-} in '') ;; --built) built=1 ;; *) die "unknown arg: $1 (only --built)" ;; esac

# S1: series lines are existing patches, every patch is in series once, manifest rows cover exactly the series.
mapfile -t series < <(grep -v '^[[:space:]]*$' "$here/series")
for p in "${series[@]}"; do [[ $p == *.patch && -f $here/$p ]] || die "series names a missing patch: $p"; done
dup=$(printf '%s\n' "${series[@]}" | sort | uniq -d)
[[ -z $dup ]] || die "series lists twice: $dup"
for f in "$here"/*.patch; do
  printf '%s\n' "${series[@]}" | grep -qxF "${f##*/}" || die "${f##*/} is not in series"
done
mapfile -t rows < <(grep -v '^#' "$here/markers.tsv" | grep -v '^[[:space:]]*$')
for r in "${rows[@]}"; do
  IFS=$'\t' read -r p kind path marker extra <<<"$r"
  [[ -n $marker && -z ${extra:-} && $kind =~ ^(src|bin)$ && -n $path ]] || die "markers.tsv: bad row: $r"
  printf '%s\n' "${series[@]}" | grep -qxF "$p" || die "markers.tsv names a patch outside series: $p"
done
for p in "${series[@]}"; do
  printf '%s\n' "${rows[@]}" | grep -qF "$p"$'\tsrc\t' || die "markers.tsv has no src row for $p"
done
echo "series/manifest OK (${#series[@]} patches, ${#rows[@]} markers)"

# S5: the smoke test must never command the wheels or the lift.
smoke=()
for f in "$here/smoke.sh" "$repo/chipper/pkg/wirepod/sdkapp/fork_smoke.go"; do [[ ! -f $f ]] || smoke+=("$f"); done
if ((${#smoke[@]})); then
  hits=$(grep -nE 'move_wheels|move_lift|DriveWheels|DriveStraight|MoveLift' "${smoke[@]}" || true)
  [[ -z $hits ]] || die "smoke code commands motion: $hits"
fi
echo "smoke motion-free OK (${#smoke[@]} files)"

# S-src: S2/S3 need the victor clone; never clone from a check.
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$repo/.git")")
src=${VICTOR_SRC:-$main/robot/work/wire-os-victor}
if [[ ! -d $src/.git ]]; then
  ((!built)) || die "--built needs the victor clone at $src"
  echo "SKIP: no victor clone at $src (patch re-export, src markers)"; exit 0
fi

# S2: *.patch + series are a byte-identical re-export of pin + series.
tip=$(VICTOR_SRC=$src "$here/apply.sh" --check) || die "apply.sh --check"
pin=$(sed -n 's/^pin=//p' "$here/apply.sh")
echo "patch re-export OK (tip ${tip:0:12})"

# S3: each src marker is in the patched tree and not at pin (so it proves that patch landed).
n=0
for r in "${rows[@]}"; do
  IFS=$'\t' read -r p kind path marker <<<"$r"
  [[ $kind == src ]] || continue
  grep -qF -- "$marker" <(git -C "$src" show "$tip:$path" 2>/dev/null) || die "$p: src marker not in $path: $marker"
  ! grep -qF -- "$marker" <(git -C "$src" show "$pin:$path" 2>/dev/null) || die "$p: src marker already at pin in $path: $marker"
  n=$((n + 1))
done
echo "src markers OK ($n)"
((built)) || exit 0

# S4: each bin marker is in the built file, and every patched JSON is copied into _build unchanged.
build=$src/_build/vicos/Release
n=0
for r in "${rows[@]}"; do
  IFS=$'\t' read -r p kind path marker <<<"$r"
  [[ $kind == bin ]] || continue
  [[ -f $build/$path ]] || die "$p: not built: $build/$path"
  grep -qF -- "$marker" <(strings -a "$build/$path") || die "$p: bin marker not in $path: $marker"
  n=$((n + 1))
done
m=0
while read -r f; do
  [[ $f == *.json ]] || continue
  cmp -s <(git -C "$src" show "$tip:$f") "$build/data/assets/cozmo_$f" || die "stale in _build: $f"
  m=$((m + 1))
done < <(git -C "$src" diff --name-only "$pin" "$tip")
echo "built markers OK ($n), built JSON = patched ($m)"

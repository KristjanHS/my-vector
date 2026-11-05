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
voice_go=() # fork_say speaks through myvector/voice
for f in "$repo"/chipper/pkg/myvector/voice/*.go; do [[ $f == *_test.go ]] || voice_go+=("$f"); done
for f in "$here/smoke.sh" "$repo/chipper/pkg/wirepod/sdkapp/fork_smoke.go"; do [[ ! -f $f ]] || smoke+=("$f"); done
[[ ! -f $here/smoke.sh || (${#smoke[@]} == 2 && ${#voice_go[@]} -gt 0) ]] || die "smoke.sh present but fork_smoke.go or myvector/voice/*.go missing (S5 would skip it)"
smoke+=("${voice_go[@]}")
if ((${#smoke[@]})); then
  hits=$(grep -nE 'move_wheels|move_lift|DriveWheels|DriveStraight|MoveLift' "${smoke[@]}" || true)
  [[ -z $hits ]] || die "smoke code commands motion: $hits"
fi
if ((${#smoke[@]} >= 3)); then
  # S5 allowlist (comment lines skipped). smoke.sh: each `sdk <name>` and api-sdk/<name> names a listed endpoint
  # literally; sdk()'s own api-sdk/$1 is the one non-literal; cloud_intent only in intent(), whose calls pass my_vector_log_*.
  sdk_ok='conn_test|get_battery|assume_behavior_control|release_behavior_control|move_head|fork_robot_state|fork_face|fork_say'
  tok='([^[:space:]);|&<>'\''`]*)' # one shell word, up to the next separator
  url_re='api-sdk(/[^?"'\''[:space:]]*)?(.*)'
  n=0
  while IFS= read -r l; do
    n=$((n + 1))
    [[ ! $l =~ ^[[:space:]]*# ]] || continue
    def=; [[ ! $l =~ ^[[:space:]]*(sdk|intent)\(\) ]] || def=${BASH_REMATCH[1]}
    rest=$l
    while [[ $rest =~ (^|[^[:alnum:]_.-])sdk[[:space:]]+$tok(.*) ]]; do
      e=${BASH_REMATCH[2]} rest=${BASH_REMATCH[3]}
      [[ $e =~ ^($sdk_ok)$ ]] || die "S5: smoke.sh:$n: sdk endpoint '$e' not a listed literal: $l"
    done
    rest=$l
    while [[ $rest =~ (^|[^[:alnum:]_.-])intent[[:space:]]+$tok(.*) ]]; do
      e=${BASH_REMATCH[2]} rest=${BASH_REMATCH[3]}
      [[ $e =~ ^my_vector_log_[a-z_]+$ ]] || die "S5: smoke.sh:$n: intent '$e' is not my_vector_log_*: $l"
    done
    rest=$l
    while [[ $rest =~ $url_re ]]; do
      e=${BASH_REMATCH[1]#/} rest=${BASH_REMATCH[2]}
      [[ $e =~ ^($sdk_ok)$ || ($def == sdk && $e == '$1') ||
        ($def == intent && $e == cloud_intent && $l == *'api-sdk/cloud_intent?serial=$esn&intent=$1"'*) ]] ||
        die "S5: smoke.sh:$n: api-sdk endpoint '$e' not allowed here: $l"
    done
    [[ $l != *cloud_intent* || $def == intent ]] || die "S5: smoke.sh:$n: cloud_intent outside intent(): $l"
  done <"${smoke[0]}"
  # Every Conn use: fork_smoke.go .EventStream / .DisplayFaceImageRGB / .SayText; myvector/voice .ExternalAudioStreamPlayback / .SayText.
  conn_ok() { # conn_ok <file> <method ERE>
    local n=0 l rest
    while IFS= read -r l; do
      n=$((n + 1))
      [[ ! $l =~ ^[[:space:]]*// ]] || continue
      rest=$l
      while [[ $rest =~ (^|[^[:alnum:]_])Conn(([^[:alnum:]_].*)?)$ ]]; do
        rest=${BASH_REMATCH[2]}
        [[ $rest =~ ^\.($2)([^[:alnum:]_]|$) ]] || die "S5: ${1##*/}:$n: Conn use other than .${2//|/\/.}: $l"
      done
    done <"$1"
  }
  conn_ok "${smoke[1]}" 'EventStream|DisplayFaceImageRGB|SayText'
  for f in "${voice_go[@]}"; do conn_ok "$f" 'ExternalAudioStreamPlayback|SayText'; done
fi
echo "smoke motion-free OK (${#smoke[@]} files; denylist + endpoint/method allowlist)"

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

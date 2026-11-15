#!/usr/bin/env bash
# make prod-check: find the first 7 consecutive completed days in robot/work/usage.tsv with real >= 5, unplanned_restarts = 0
# and tailnet = up, and seal it once as the annotated tag prod/start on HEAD (body = ledger header + the 7 rows), pushed to origin.
# Manual only (prod-evidence ruling): nothing runs it on a timer. Today's row is never used — the day is not over.
# A prod/start that already exists is never moved: exit 0 if its body equals the recomputed window, 1 if not.
# Exit 0 sealed/already sealed, 3 no window yet (prints the longest run), 1 error.
# Env (tests): PROD_REPO (default this checkout), PROD_LEDGER (default <repo>/robot/work/usage.tsv), PROD_TODAY (YYYY-MM-DD),
#   PROD_NO_PUSH=1 (tag only).
set -euo pipefail
repo=${PROD_REPO:-$(cd "$(dirname "$0")/.." && pwd)}
ledger=${PROD_LEDGER:-$repo/robot/work/usage.tsv}
today=${PROD_TODAY:-$(date +%F)}
days=7 min_real=5 tag=prod/start
die() { echo "FAIL: prod-check: $*" >&2; exit 1; }
[[ -f $ledger ]] || { echo "no ledger yet ($ledger): wire-pod writes it once the usage counter ships"; exit 3; }

# One awk pass: rows before today, sorted by day; a run grows while each day qualifies and follows the previous by one
# calendar day. Prints "found" + the 7 raw rows, or "best <n> <first day>".
win=$(sort -k1,1 <(tail -n +2 "$ledger") | awk -F'\t' -v today="$today" -v need="$days" -v min="$min_real" '
  # days since the civil epoch (Hinnant days_from_civil) — pure arithmetic, no gawk mktime
  function jd(d,  a, y, m, era, yoe, doy) {
    split(d, a, "-"); y = a[1] - (a[2] <= 2); m = a[2] + 0
    era = int(y / 400); yoe = y - era * 400
    doy = int((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + a[3] - 1
    return era * 146097 + yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy
  }
  $1 >= today { next }
  {
    ok = ($2 + 0 >= min && $4 == "0" && $5 == "up")
    if (!ok) { n = 0; next }
    if (n > 0 && jd($1) - jd(prev) == 1) { n++ } else { n = 1; first = $1 }
    rows[n] = $0; prev = $1
    if (n > best) { best = n; bfirst = first }
    if (n == need) { print "found"; for (i = 1; i <= need; i++) print rows[i]; exit }
  }
  END { if (n != need) print "best " (best + 0) " " (bfirst == "" ? "-" : bfirst) }')

if [[ $win != found* ]]; then
  read -r _ n first <<<"$win"
  echo "no $days-day window yet: longest qualifying run $n day(s)${first:+ from $first} (each day: real >= $min_real, unplanned_restarts = 0, tailnet = up)"
  exit 3
fi
body=$(printf 'prod start %s\n\n%s\n%s\n' "$(sed -n 2p <<<"$win" | cut -f1)" "$(head -1 "$ledger")" "$(tail -n +2 <<<"$win")")
[[ ! $body =~ [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ ]] || die "window body carries a dotted quad: ${BASH_REMATCH[0]}"
printf '%s\n' "$body"

if git -C "$repo" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  have=$(git -C "$repo" tag -l --format='%(contents)' "$tag")
  [[ ${have%$'\n'} == "$body" ]] || die "$tag exists with a different body (never moved) — ledger rewritten? have:"$'\n'"$have"
  echo "$tag already sealed; the ledger reproduces its body"
  exit 0
fi
git -C "$repo" tag -a -m "$body" "$tag" HEAD || die "could not create $tag"
echo "tagged $tag on $(git -C "$repo" rev-parse --short HEAD)"
if [[ -z ${PROD_NO_PUSH:-} ]]; then
  git -C "$repo" push origin "refs/tags/$tag" || die "push failed — the tag is local; push by hand: git push origin refs/tags/$tag"
  echo "pushed $tag to origin"
fi

#!/usr/bin/env bash
# make prod-check: find the first 7 consecutive FINAL days in robot/work/usage.tsv with real >= 5, unplanned_restarts = 0
# and tailnet = up, and seal it once as the annotated tag prod/start (body = ledger header + the 7 rows), pushed to origin.
# Manual only (prod-evidence ruling): nothing runs it on a timer.
# Final day = before today AND before the day of wire-pod's last saved robot poll (robot/work/usage-poll.state mtime): the poll
#   books a restart on the day it happened, so a day is only closed once a poll has run after it.
# The tag goes on origin/main (what the last ship pushed), never a local-only HEAD: pushing a tag publishes its history.
# A prod/start that already exists is never moved: exit 0 if the ledger reproduces its body (or the ledger is gone), 1 if not;
#   a local-only one is pushed.
# Exit 0 sealed/already sealed, 3 no window yet (prints the longest run), 1 error.
# Env (tests): PROD_REPO (default the main checkout), PROD_LEDGER / PROD_STATE (default <main>/robot/work/{usage.tsv,usage-poll.state}),
#   PROD_TODAY (YYYY-MM-DD), PROD_NO_PUSH=1 (tag only).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)") # robot/work lives in the main checkout
repo=${PROD_REPO:-$main}
ledger=${PROD_LEDGER:-$main/robot/work/usage.tsv}
state=${PROD_STATE:-$main/robot/work/usage-poll.state}
today=${PROD_TODAY:-$(date +%F)}
days=7 min_real=5 tag=prod/start
die() { echo "FAIL: prod-check: $*" >&2; exit 1; }
push_tag() {
  [[ -z ${PROD_NO_PUSH:-} ]] || return 0
  git -C "$repo" push origin "refs/tags/$tag" || die "push failed — the tag is local; push by hand: git push origin refs/tags/$tag"
  echo "pushed $tag to origin"
}
sealed=0; git -C "$repo" rev-parse -q --verify "refs/tags/$tag" >/dev/null && sealed=1
if ((sealed)) && [[ ! -f $ledger ]]; then
  echo "$tag already sealed; no ledger here to re-verify it"
  if [[ -z ${PROD_NO_PUSH:-} ]] && ! git -C "$repo" ls-remote --exit-code origin "refs/tags/$tag" >/dev/null; then push_tag; fi
  exit 0
fi
[[ -f $ledger ]] || { echo "no ledger yet ($ledger): wire-pod writes it once the usage counter ships"; exit 3; }
cutoff=$today
if [[ -f $state ]]; then polled=$(date -r "$state" +%F); [[ $polled > $cutoff ]] || cutoff=$polled
else cutoff=0000-00-00; fi # no robot poll yet: no day is final

# One awk pass: final rows, sorted by day; a run grows while each day qualifies and follows the previous by one
# calendar day. Prints "found" + the 7 raw rows, or "best <n> <first day>".
win=$(sort -k1,1 <(tail -n +2 "$ledger") | awk -F'\t' -v cutoff="$cutoff" -v need="$days" -v min="$min_real" '
  # days since the civil epoch (Hinnant days_from_civil) — pure arithmetic, no gawk mktime
  function jd(d,  a, y, m, era, yoe, doy) {
    split(d, a, "-"); y = a[1] - (a[2] <= 2); m = a[2] + 0
    era = int(y / 400); yoe = y - era * 400
    doy = int((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + a[3] - 1
    return era * 146097 + yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy
  }
  $1 >= cutoff { next }
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
  ((sealed == 0)) || die "$tag exists but the ledger no longer holds its window (never moved) — ledger rewritten?"
  read -r _ n first <<<"$win"
  echo "no $days-day window yet: longest qualifying run $n final day(s)${first:+ from $first} (each day: real >= $min_real, unplanned_restarts = 0, tailnet = up; final = before $cutoff)"
  exit 3
fi
body=$(printf 'prod start %s\n\n%s\n%s\n' "$(sed -n 2p <<<"$win" | cut -f1)" "$(head -1 "$ledger")" "$(tail -n +2 <<<"$win")")
[[ ! $body =~ [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ ]] || die "window body carries a dotted quad: ${BASH_REMATCH[0]}"
printf '%s\n' "$body"

if ((sealed)); then
  have=$(git -C "$repo" tag -l --format='%(contents)' "$tag")
  [[ ${have%$'\n'} == "$body" ]] || die "$tag exists with a different body (never moved) — ledger rewritten? have:"$'\n'"$have"
  echo "$tag already sealed; the ledger reproduces its body"
  if [[ -z ${PROD_NO_PUSH:-} ]] && ! git -C "$repo" ls-remote --exit-code origin "refs/tags/$tag" >/dev/null; then push_tag; fi
  exit 0
fi
at=$(git -C "$repo" rev-parse -q --verify refs/remotes/origin/main) || die "no origin/main to tag (git fetch origin first)"
git -C "$repo" tag -a -m "$body" "$tag" "$at" || die "could not create $tag"
echo "tagged $tag on origin/main $(git -C "$repo" rev-parse --short "$at")"
push_tag

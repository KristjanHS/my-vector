#!/usr/bin/env bash
# Host test of scripts/lib/check-runner.sh (make check): green prints in queue order; a failure prints its log and kills the rest.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$here/../../../.git")")
tmp=$(mktemp -d "$main/robot/work/check-runner-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: check-runner host test: $*" >&2; exit 1; }
lib=$here/../lib/check-runner.sh

out=$(. "$lib"; job a sleep 0.3; job b echo hi-b; run_jobs "$tmp") || die "green run failed: $out"
[[ $(awk '{print $1, $2}' <<<"$out" | paste -sd,) == "ok a,ok b" ]] || die "summary not in queue order: $out"
grep -q 'hi-b$' <<<"$out" || die "summary lacks the job's last log line: $out"

# slow's grandchild lingers 0.3 s after TERM, so run_jobs returning before it is gone leaves it to pgrep; bad fails once it is up
m=$RANDOM.$$ ready=$tmp/ready SECONDS=0
linger='trap "sleep 0.3; exit 0" TERM; sleep 31 & touch "$1"; wait'
out=$(. "$lib"; job slow sh -c 'sh -c "$1" "linger-$2" "$3" & wait' sh "$linger" "$m" "$ready"
  job bad sh -c 'until [ -e "$1" ]; do sleep 0.01; done; echo boom; exit 3' sh "$ready"; run_jobs "$tmp" 2>&1) && die "red run passed: $out"
((SECONDS < 5)) || die "red run took ${SECONDS}s: the slow job was waited for, not killed"
! pgrep -f "linger-$m" >/dev/null || die "slow job's process group outlived run_jobs"
grep -q '^killed *slow' <<<"$out" || die "slow job not reported killed: $out"
grep -q '^boom$' <<<"$out" || die "failing job's log not printed: $out"
echo "check-runner tests OK"

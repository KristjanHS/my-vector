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

m=31.$RANDOM SECONDS=0
out=$(. "$lib"; job slow sleep "$m"; job bad sh -c 'echo boom; exit 3'; run_jobs "$tmp" 2>&1) && die "red run passed: $out"
((SECONDS < 5)) || die "red run took ${SECONDS}s: the slow job was waited for, not killed"
! pgrep -f "sleep $m" >/dev/null || die "slow job survived the kill"
grep -q '^killed *slow' <<<"$out" || die "slow job not reported killed: $out"
grep -q '^boom$' <<<"$out" || die "failing job's log not printed: $out"
echo "check-runner tests OK"

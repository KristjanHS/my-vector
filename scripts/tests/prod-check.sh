#!/usr/bin/env bash
# Host test of scripts/prod-check.sh (make check): ledgers in a throwaway repo with a bare origin; PROD_TODAY pinned.
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
nbad=0; fail() { echo "FAIL prod-check: $*" >&2; nbad=$((nbad + 1)); }
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
git init -q --bare "$t/origin.git"
git init -q "$t/repo"; git -C "$t/repo" commit -q --allow-empty -m one; git -C "$t/repo" remote add origin "$t/origin.git"
H=$'day\treal\ttest\tunplanned_restarts\ttailnet'
row() { printf '%s\t%s\t0\t%s\t%s\n' "$1" "${2:-6}" "${3:-0}" "${4:-up}"; } # day [real] [restarts] [tailnet]
good() { local d; for d; do row "$d"; done; }
run() { # <ledger body> -> sets rc + out
  printf '%s\n%s' "$H" "$1" >"$t/usage.tsv"
  rc=0; out=$(PROD_REPO=$t/repo PROD_LEDGER=$t/usage.tsv PROD_TODAY=${today:-2026-12-01} PROD_NO_PUSH=${nopush-1} \
    bash "$dir/scripts/prod-check.sh" 2>&1) || rc=$?
}
wk=(2026-11-02 2026-11-03 2026-11-04 2026-11-05 2026-11-06 2026-11-07 2026-11-08)

run "$(good "${wk[@]:0:6}")"; ((rc == 3)) || fail "6 good days: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 4; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a real = 4 day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 9 1; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a restart day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 9 0 -; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a tailnet '-' day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}" "${wk[@]:4:3}" 2026-11-09)"; ((rc == 3)) || fail "a missing calendar day: rc $rc, want 3: $out"
# month boundary counts as consecutive; today's row is never used
run "$(good 2026-11-25 2026-11-26 2026-11-27 2026-11-28 2026-11-29 2026-11-30 2026-12-01)"; ((rc == 3)) || fail "today counted: rc $rc: $out"
[[ -z $(git -C "$t/repo" tag -l prod/start) ]] || fail "a non-window created prod/start"

# 7 good days, out of order in the file, after a broken earlier run: the tag body = header + exactly those rows
run "$(good "${wk[@]:3:4}"; row 2026-11-01 2; good "${wk[@]:0:3}"; row 2026-11-09 1)"
((rc == 0)) || fail "7 good days: rc $rc, want 0: $out"
want=$(printf 'prod start %s\n\n%s\n%s' "${wk[0]}" "$H" "$(good "${wk[@]}")")
have=$(git -C "$t/repo" tag -l --format='%(contents)' prod/start); have=${have%$'\n'}
[[ $have == "$want" ]] || fail "tag body:"$'\n'"$have"$'\n'"want:"$'\n'"$want"
[[ $(git -C "$t/repo" cat-file -t prod/start) == tag ]] || fail "prod/start is not annotated"

# re-run: the same ledger reproduces the body -> 0, never re-tagged; a rewritten row -> 1
run "$(good "${wk[@]}")"; ((rc == 0)) && [[ $out == *"already sealed"* ]] || fail "re-run: rc $rc: $out"
run "$(good "${wk[@]:0:6}"; row "${wk[6]}" 7)"; ((rc == 1)) || fail "rewritten ledger: rc $rc, want 1: $out"

# push to origin
git -C "$t/repo" tag -d prod/start >/dev/null
nopush='' run "$(good "${wk[@]}")"; ((rc == 0)) || fail "push run: rc $rc: $out"
git -C "$t/origin.git" rev-parse -q --verify refs/tags/prod/start >/dev/null || fail "prod/start not pushed to origin"

((nbad == 0)) || exit 1
echo "ok: prod-check (window rules, seal body, never moved, push)"

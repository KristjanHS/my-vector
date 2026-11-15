#!/usr/bin/env bash
# Host test of scripts/prod-check.sh (make check): ledgers in a throwaway repo with a bare origin; PROD_TODAY and the
# poll-state mtime pinned.
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
nbad=0; fail() { echo "FAIL prod-check: $*" >&2; nbad=$((nbad + 1)); }
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
git init -q --bare "$t/origin.git"
r=$t/repo
git init -q -b main "$r"; git -C "$r" commit -q --allow-empty -m shipped; git -C "$r" remote add origin "$t/origin.git"
git -C "$r" push -q origin main; shipped=$(git -C "$r" rev-parse HEAD)
git -C "$r" commit -q --allow-empty -m local-only # never pushed: prod/start must not publish it
H=$'day\treal\ttest\tunplanned_restarts\ttailnet'
row() { printf '%s\t%s\t0\t%s\t%s\n' "$1" "${2:-6}" "${3:-0}" "${4:-up}"; } # day [real] [restarts] [tailnet]
good() { local d; for d; do row "$d"; done; }
polled=2026-12-01 # last saved robot poll (state mtime)
run() { # <ledger body> -> sets rc + out
  printf '%s\n%s' "$H" "$1" >"$t/usage.tsv"
  touch -d "$polled 08:00" "$t/state"
  rc=0; out=$(PROD_REPO=$r PROD_LEDGER=$t/usage.tsv PROD_STATE=$t/state PROD_TODAY=${today:-2026-12-01} PROD_NO_PUSH=${nopush-1} \
    bash "$dir/scripts/prod-check.sh" 2>&1) || rc=$?
}
wk=(2026-11-02 2026-11-03 2026-11-04 2026-11-05 2026-11-06 2026-11-07 2026-11-08)

run "$(good "${wk[@]:0:6}")"; ((rc == 3)) || fail "6 good days: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 4; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a real = 4 day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 9 1; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a restart day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}"; row "${wk[3]}" 9 0 -; good "${wk[@]:4:3}")"; ((rc == 3)) || fail "a tailnet '-' day: rc $rc, want 3: $out"
run "$(good "${wk[@]:0:3}" "${wk[@]:4:3}" 2026-11-09)"; ((rc == 3)) || fail "a missing calendar day: rc $rc, want 3: $out"
# today's row is never final; nor is a day the last poll has not passed
run "$(good 2026-11-25 2026-11-26 2026-11-27 2026-11-28 2026-11-29 2026-11-30 2026-12-01)"; ((rc == 3)) || fail "today counted: rc $rc: $out"
polled=2026-11-30 today=2026-12-03 run "$(good 2026-11-24 2026-11-25 2026-11-26 2026-11-27 2026-11-28 2026-11-29 2026-11-30)"
((rc == 3)) || fail "a day after the last poll counted: rc $rc: $out"
printf '%s\n%s' "$H" "$(good "${wk[@]}")" >"$t/usage.tsv"
rc=0; out=$(PROD_REPO=$r PROD_LEDGER=$t/usage.tsv PROD_STATE=$t/none PROD_TODAY=2026-12-01 PROD_NO_PUSH=1 bash "$dir/scripts/prod-check.sh" 2>&1) || rc=$?
((rc == 3)) || fail "no poll state yet: rc $rc, want 3: $out"
[[ -z $(git -C "$r" tag -l prod/start) ]] || fail "a non-window created prod/start"

# a month boundary is consecutive (today and the last poll past it)
today=2026-12-03 polled=2026-12-03 run "$(good 2026-11-26 2026-11-27 2026-11-28 2026-11-29 2026-11-30 2026-12-01 2026-12-02)"
((rc == 0)) && [[ $out == "prod start 2026-11-26"* ]] || fail "month boundary: rc $rc: $out"
git -C "$r" tag -d prod/start >/dev/null 2>&1 || true

# 7 good days, out of order in the file, after a broken earlier run: the tag body = header + exactly those rows, on origin/main
run "$(good "${wk[@]:3:4}"; row 2026-11-01 2; good "${wk[@]:0:3}"; row 2026-11-09 1)"
((rc == 0)) || fail "7 good days: rc $rc, want 0: $out"
want=$(printf 'prod start %s\n\n%s\n%s' "${wk[0]}" "$H" "$(good "${wk[@]}")")
have=$(git -C "$r" tag -l --format='%(contents)' prod/start); have=${have%$'\n'}
[[ $have == "$want" ]] || fail "tag body:"$'\n'"$have"$'\n'"want:"$'\n'"$want"
[[ $(git -C "$r" cat-file -t prod/start 2>&1) == tag ]] || fail "prod/start is not annotated"
[[ $(git -C "$r" rev-parse 'prod/start^{commit}' 2>&1) == "$shipped" ]] || fail "prod/start is not on origin/main (a local-only HEAD would be published)"

# re-run: the same ledger reproduces the body -> 0, never re-tagged; a rewritten row -> 1; a window gone -> 1; ledger gone -> 0
run "$(good "${wk[@]}")"; ((rc == 0)) && [[ $out == *"already sealed"* ]] || fail "re-run: rc $rc: $out"
run "$(good "${wk[@]:0:6}"; row "${wk[6]}" 7)"; ((rc == 1)) || fail "rewritten ledger: rc $rc, want 1: $out"
run "$(good "${wk[@]:0:6}")"; ((rc == 1)) || fail "sealed but no window: rc $rc, want 1: $out"
rc=0; out=$(PROD_REPO=$r PROD_LEDGER=$t/none PROD_STATE=$t/state PROD_NO_PUSH=1 bash "$dir/scripts/prod-check.sh" 2>&1) || rc=$?
((rc == 0)) || fail "sealed, ledger gone: rc $rc, want 0: $out"

# a local-only prod/start (an earlier push failed) is pushed by the re-run
nopush='' run "$(good "${wk[@]}")"; ((rc == 0)) || fail "push re-run: rc $rc: $out"
git -C "$t/origin.git" rev-parse -q --verify refs/tags/prod/start >/dev/null || fail "a local-only prod/start was not pushed on re-run"

((nbad == 0)) || exit 1
echo "ok: prod-check (window rules, final days, seal body on origin/main, never moved, push)"

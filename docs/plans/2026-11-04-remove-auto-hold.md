# Remove auto-hold: the firmware patches keep Vector docked

**Goal**: delete the server-side auto-hold (`AUTO_HOLD`); the patched firmware alone keeps him on the charger, with
"Hey Vector" working and no wire-pod dependency. **Gate**: start only after the soak and Q6 checks in
`docs/backlog.md` §OWED[USER] pass (verdict due after 2026-11-05 08:12). FAIL → don't run this plan; A1–A5 in
2026-11-04-architecture-refactors.md (docs/plans/) shrink auto-hold instead. **Status**: written, not started.

## Rulings

| id | ruling | edit site |
|---|---|---|
| H1 | Delete the package and its glue | `chipper/pkg/myvector/autohold/` (whole dir), `chipper/pkg/wirepod/sdkapp/fork_autohold.go` |
| H2 | Remove the `InitAutoHold()` call so `server.go` is byte-identical to upstream again, and drop its `allowed_edits` entry | `chipper/pkg/wirepod/sdkapp/server.go` `BeginServer`, `scripts/upstream-drift.sh` `allowed_edits` |
| H3 | Keep the `bcassume.go` busy-spin fix and its `allowed_edits` entry: it fixes upstream's web-UI Assume, which `robot/unlock/power-stress.sh` still uses | — |
| H4 | `go test ./pkg/myvector/...` exits 1 once the dir is gone (measured: a missing pattern dir → rc=1). Run it only when `chipper/pkg/myvector/` exists, and add `go vet ./pkg/wirepod/sdkapp/` so the `bcassume.go` edit stays compiled (this ships R1) | `scripts/check.sh`, `Makefile` `check` help text |
| H5 | `power-stress.sh`'s header says "AUTO_HOLD=override", but it holds through `assume_behavior_control priority=high`; rewrite the header to say so | `robot/unlock/power-stress.sh` header |
| H6 | Keep the `chipper/pkg/myvector/` + `fork_*.go` convention in `CLAUDE.md` and `upstream-drift.sh` for the next fork feature | — |
| H7 | On ship: delete R4 and A1–A5 (and R1 if H4 shipped it) from 2026-11-04-architecture-refactors.md (docs/plans/), drop R4's step from its Order line, delete the "Only if the soak FAILs: live-check auto-hold" line and the PASS → pointer to this plan in `docs/backlog.md`; archive this plan | `docs/plans/2026-11-04-architecture-refactors.md`, `docs/backlog.md` |

## Checklist

- [ ] Gate: `robot/stay-on-charger/soak.sh --summary` → `PASS`, and one voice command that moves him (Q6) leaves him docked.
- [ ] H1 + H2 + H4 + H5 in one commit, H7 in the same commit.
- [ ] `grep -rn --exclude-dir=.git --exclude-dir=work --exclude-dir=archive -i 'autohold\|AUTO_HOLD\|InitAutoHold' .` → only `robot/unlock/checklist.html` (historical "unset").
- [ ] `git diff $(git merge-base HEAD upstream/main) -- chipper/pkg/wirepod/sdkapp/server.go` → empty; `make check` green (drift reports 2 allowed edits).
- [ ] `[USER]` restart chipper (`make start`): log has no `Auto-hold` line; "Hey Vector" answers; web-UI Assume/Release still work.

## Rollback

`git revert` the removal commit, set `export AUTO_HOLD=override` in `chipper/source.sh`, restart chipper.

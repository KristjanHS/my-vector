---
name: Doc writing — moves, not narrative
description: What an unattended write to docs/**.md may admit, and the delete that must ride with every add
paths:
  - "docs/**/*.md"
---
# Doc writing — moves, not narrative

The two-line unit and supersede-by-replacing are global → `~/.claude/rules/prose-budget.md`. This file
carries only what is project-specific.

An observation you were not asked to write down goes in the **commit message**. It goes in a file only when
it (a) supersedes a line already there — rewrite that line — or (b) can be spelled as a value, a symbol, or
a command that returns non-zero. A net-new comment, docblock, `##` section or test name that supersedes
nothing and spells as none of those is not shortened; it is not written.

Write a value change into the source (`.go` / config) — the VALUE, plus at most the one line that binds the
next edit; displacing a doc's narrative into a code comment is the violation, not the fix. Write into the doc
the one line that binds the next edit, **and delete the line it supersedes** — `git show` keeps it.

⚠ **Check inbound cites before deleting** — repoint any cite to the deleted text in the same commit
(`plan-hygiene.md` §Deleting a SECTION HEADING dangles refs exactly like deleting a file).

## The writable set — each target with one admission row

Every `.md` outside the table is **read-only** — `docs/plans/archive/**` and upstream wire-pod docs
(`README.md` etc.; editing them causes merge conflicts with `upstream`). A write to any of them is the defect.

| target | admits | refuses |
|---|---|---|
| `docs/backlog.md` | **one 2-line unit**: ⚠ symptom · seam · falsifier (a COMMAND) — **only if someone would act on it now**. **Deleted in the commit that ships it** | narrative, rationale, an item with no seam; anything ruled not-worth-doing |
| `docs/backlog.md` §OWED[USER] | one line per open decision, deleted when ruled | the options, the trade-offs (→ the asking message) |
| `docs/plans/**` | staged decisions and the edits that implement them — `id \| ruling \| edit site` | session narrative, measurement dumps, rejected-option prose, findings restated from the commit |

**The source stays the only home of every VALUE.** A number reaches a doc only as a measured input the code
cites and cannot itself carry.

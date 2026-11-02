---
paths:
  - "docs/**/*.md"
---
# Plan / Doc Hygiene

## Archiving and deleting a plan doc

- **Archive at ship** — every stage committed, only a USER-owed manual/live confirm left: `git mv` the plan
  into `docs/plans/archive/`. User-owed *decisions* go to `docs/backlog.md` §OWED[USER]; assistant-owed
  follow-ups go to `docs/backlog.md`, deleted in the commit that ships them. Never record "archive-pending".
- **A carry is a re-admission, never a relocation** — an item moved into `docs/backlog.md` re-clears its
  admission row (`doc-writing.md`) at the move: the 2-line unit lands; evidence, measurements and ship
  narrative stay in the archived plan and the commit message.
- `git add` uncommitted edits to the doc **first** (`git mv` stages against HEAD and silently drops them),
  and **pathspec both halves of the rename** — naming only the archive path leaves the doc at both paths.
- The archiving commit sweeps every test name citing the plan's stem: move it to the owning seam with the
  stem dropped, or delete it.
- Never `git rm` a plan doc — archiving keeps every stem cite resolving. Commit a staged plan doc before the
  session ends: untracked files don't reach worktrees.

## No global execution driver

Live plans self-sequence; the next one is a user / `qimpag` pick — never author a master queue or sequence doc.

## Cite plan docs by stem, not path

Cite `<stem>.md (docs/plans/)` — never a `docs/plans/`-prefixed path, and never into `docs/plans/archive/`
(a repoint there re-breaks on the next move). Archiving then dangles nothing.

## Claims

Everything you write down is a claim. The moves:

- **Cite constants by SYMBOL, never `file.go:LINE`** — line cites rot within a day.
- **A coverage claim owes a grep of the named test** — including the negative form: "nothing pins X" means
  grepping the VALUE, not just a symbol.
- **A recorded SHA owes `git merge-base --is-ancestor <sha> main`.** SHAs in plan headers are unreliable —
  prove landing by subject (`git log --oneline main --grep '<stem-or-symbol>'`) or by the symbol in the tree.
- **An audit or review finding is true at capture only** — re-grep the mechanism before acting on it later.
- **Moving a section — or a `describe` block into a new file — RE-ASSERTS every claim inside it.** Grep the
  moved text for `this file`, `above`/`below`, counts and its outbound cites, **resolve each locator to the
  file that now holds its referent** (contiguity is not resolution — a half that stayed whole still points
  at the other half), and diff it against the original for silently dropped GATES.
- **A gate or quantifier you author owes the population it must admit** — run it exactly as the doc writes
  it; it can be unsatisfiable by construction.
- **Closing a decision — or shipping any owed item — owes a stem-grep of its staged copies** — `project_state`,
  `docs/backlog.md` (§OWED[USER] included) — in the same burst: **delete the entry; a
  ✅/CLOSED marker written into the file is the defect** — the commit message is the closure record.

An unasked lesson goes where `doc-writing.md` sends it: the commit message.

## Deleting a SECTION HEADING dangles refs exactly like deleting a file

Nothing warns — the file still resolves. On a rename, merge or deletion of a `##`, sweep the bare STEM
repo-wide and re-read every hit against the surviving text, **in the same commit** (`docs/plans/archive/`
included):

```
grep -rn '<file-stem>' --include=*.md --include=*.go --include=*.js --include=*.sh
```

The bare stem subsumes every `§`-anchored form and catches the largest residue — cites that invoke the target
with no `§` at all, which any `§`-anchored grep misses by construction. **A `§` cite must always carry its
stem** (nothing resolves *which* doc otherwise). Before citing a `§`, confirm the target is a real `##` —
`grep -n '^#\+ ' <file>`; a **bold paragraph** reads like a heading in every other respect, so a cite onto
one dangles from birth. **So does a cite whose target the SAME commit deletes** — when one commit both
adds a `§` cite (a docblock, a plan line) and removes the section it points at, nothing in the pre-commit
tree looks wrong; run the stem grep against the **post-commit** tree, or read the cite against the diff's
`+`/`-` halves together.

## A mechanical cite sweep must not touch COMMAND ARGS — and a rewrite owes its own verification

1. **A `sed` that drops a path prefix cannot tell a cite from an argument.** `git mv docs/plans/<x>.md
   docs/plans/archive/` inside an archived doc is a *command*, not a cite — destemmed, it no longer
   resolves from repo root. Before a prefix sweep, exclude the command forms (`git mv `, `git rm `,
   `cat `, `grep `, `[ -f `) or re-grep for them afterwards and restore the prefix.
2. **Replacing a rotted claim can produce one that is false in the OTHER direction.** A rewritten claim
   carries the same burden as a new one: derive it from the code, and check the EXAMPLE separately from
   the rule it illustrates.

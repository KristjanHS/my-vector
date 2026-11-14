# Own README replacing upstream wire-pod's — portfolio slot 2 spec

- **Source:** dotfiles `docs/plans/2026-11-14-agentic-portfolio-prep.md` slot 2 item 3 (P13) + §5.3 template.
- **Run:** `/qimpag docs/plans/2026-11-14-readme-spec.md` from this repo's cwd. README-only; robot video + trails are slot 6.

## Stage 1 — own README, wire-pod credited

- Lead: what my-vector adds (LLM, voice, Estonian STT) on top of wire-pod, the Anki Vector server.
- Keep a `## Credits` section naming wire-pod + Kerigan Creighton; keep `LICENSE` (MIT, upstream copyright) untouched.
- Move the upstream install/wiki pointers under a "Base: wire-pod" section; drop the Donate section (it is upstream's).
- Falsifier: `grep -n '^# wire-pod' README.md` returns nothing, and `grep -c 'wire-pod' README.md` is ≥1.

## Stage 2 — §5.3 template slots

- Leave a demo placeholder for the 30-second robot video (slot 6) and a `## How it was built` heading with a TODO.
- No numbers, no fold/employer wording (W6, C3).

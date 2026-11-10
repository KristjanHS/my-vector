#!/bin/bash
# PostToolUse Write|Edit: gofmt -w the one .go file just written — never a package (`go fmt ./pkg/...` churns upstream files).
# Only this repo's own files: a gitignored path, a nested clone (robot/work/*) or anything outside the repo is left alone.
# Format on first edit (ruled 2026-11-10): an unformatted upstream file is fully gofmt-ed the first time the fork edits it —
# note "whole file gofmt-ed" on its scripts/upstream-drift.sh line. Misses Bash/sed edits: make check runs scripts/tests/gofmt.sh.
in=$(cat)
f=$(jq -r '.tool_input.file_path // empty' <<<"$in")
[[ "$f" == *.go && -f "$f" ]] || exit 0
gofmt=$(command -v gofmt || echo "$(go env GOROOT 2>/dev/null)/bin/gofmt")
[[ -x "$gofmt" ]] || exit 0
root=$(git -C "${CLAUDE_PROJECT_DIR:-$(dirname "$0")/../..}" rev-parse --show-toplevel 2>/dev/null) || exit 0
d=$(dirname "$f")
[[ $(git -C "$d" rev-parse --show-toplevel 2>/dev/null) == "$root" ]] || exit 0
git -C "$d" check-ignore -q -- "$(basename "$f")" && exit 0
before=$(sha1sum <"$f")
"$gofmt" -w "$f" 2>/dev/null || exit 0
[[ $(sha1sum <"$f") == "$before" ]] && exit 0
jq -n --arg f "$f" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: "gofmt -w reformatted \($f)"}}'
exit 0

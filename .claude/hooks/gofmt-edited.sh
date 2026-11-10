#!/bin/bash
# PostToolUse Write|Edit: gofmt -w the one .go file just written — never a package (`go fmt ./pkg/...` churns upstream files).
# Skipped when the file's HEAD copy isn't gofmt-clean (upstream), so its diff stays only the edit (ruff-check.sh baseline rule).
# Misses Bash/sed edits and fork files committed unformatted: make check runs scripts/tests/gofmt.sh for those.
in=$(cat)
f=$(jq -r '.tool_input.file_path // empty' <<<"$in")
[[ "$f" == *.go && -f "$f" ]] || exit 0
gofmt=$(command -v gofmt || echo "$(go env GOROOT 2>/dev/null)/bin/gofmt")
[[ -x "$gofmt" ]] || exit 0
d=$(dirname "$f")
b=$(basename "$f")
if git -C "$d" cat-file -e "HEAD:./$b" 2>/dev/null; then
  [[ -z $(git -C "$d" show "HEAD:./$b" | "$gofmt" -l 2>/dev/null) ]] || exit 0
fi
before=$(sha1sum <"$f")
"$gofmt" -w "$f" 2>/dev/null || exit 0
[[ $(sha1sum <"$f") == "$before" ]] && exit 0
jq -n --arg f "$f" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: "gofmt -w reformatted \($f)"}}'
exit 0

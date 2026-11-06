#!/usr/bin/env bash
# Fails when the fork touches an upstream wire-pod path that is not listed below.
# Fork code goes in chipper/pkg/myvector/; glue into an upstream package is a fork_*.go file.
# Untracked files are not checked: `git add` them first. --head [rev] checks only that commit (default HEAD; make ship passes its pinned sha).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Upstream files the fork edits on purpose: the only places an upstream merge can conflict.
allowed_edits=(
  chipper/pkg/wirepod/sdkapp/bcassume.go # busy-spin fix while behavior control is held
  .gitignore                             # fork block: *.key, chipper/*.bak, session-certs
  chipper/pkg/wirepod/ttr/kgsim.go       # TopP: forkTopP(...) — Claude rejects top_p (fork_llm.go); CreateAIReq returns forkAIReq(...) (fork_prompt.go); removeSpecialCharacters strips with forkMarkFilter() (fork_marks.go)
  chipper/pkg/wirepod/ttr/kgsim_cmds.go  # same, on the LLM-commands/camera path; DoSayText -> forkSayText (fork_say.go); DoGetImage defers forkCameraReq(...) (fork_prompt.go)
  chipper/pkg/vars/vars.go               # LoadIntents returns forkFilterIntents(...) (fork_intents.go); reads intent-data/<ForkLanguage()> (fork_lang.go)
  chipper/pkg/wirepod/ttr/matchIntentSend.go # ProcessTextAll partial pass: vars.ForkContainsPhrase (whole words); !locale = vars.ForkLanguage(); voiceText via vars.ForkNormalizeText
  chipper/pkg/wirepod/localization/localization.go # GetText asks forkGetText first (fork_lang.go)
  chipper/pkg/wirepod/ttr/words2num.go   # words2num input = forkTimerText(...) — et-EE timer forms (fork_timer.go)
  chipper/pkg/wirepod/ttr/weather.go     # weatherParser apiLocation = forkWeatherLocation(...) — et-EE city word (fork_weather.go)
  chipper/pkg/wirepod/preqs/server.go    # sttHandler = forkMicCapture(forkSTTRouter(...)) — mic capture (fork_miccap.go) + et-EE STT router (fork_stt.go)
  chipper/webroot/index.html             # nav link to the fork voice page /myvector/voice (myvector/voice)
)

to=(); [[ ${1:-} == --head ]] && to=("$(git rev-parse --verify "${2:-HEAD}^{commit}")")
ref=${UPSTREAM_REF:-upstream/main}
git rev-parse -q --verify "$ref^{commit}" >/dev/null || { echo "missing $ref: git fetch upstream" >&2; exit 2; }
base=$(git merge-base "${to[0]:-HEAD}" "$ref")
roots=$(git ls-tree --name-only "$base")

is_allowed() {
  local a
  for a in "${allowed_edits[@]}"; do [[ $1 == "$a" ]] && return 0; done
  return 1
}

# Working tree vs the merge-base, so uncommitted edits count too. --no-renames splits a rename into a
# delete (of the upstream path) plus an add, so both halves are checked. A failed diff aborts via set -e.
changes=$(mktemp)
trap 'rm -f "$changes"' EXIT
git diff -z --no-renames --name-status "$base" "${to[@]}" >"$changes"

bad=0
while IFS= read -r -d '' status && IFS= read -r -d '' path; do
  grep -qxF -- "${path%%/*}" <<<"$roots" || continue # fork-only top level (docs/, robot/, scripts/, ...)
  if [[ $status == A ]]; then
    # et-EE.json: upstream's LoadIntents reads intent-data/<lang>.json, so the fork's table must sit there
    [[ $path == chipper/pkg/myvector/* || $(basename "$path") == fork_*.go || $path == chipper/intent-data/et-EE.json ]] && continue
    echo "new fork file inside the upstream tree: $path (move it to chipper/pkg/myvector/ or name it fork_*.go)"
  else
    is_allowed "$path" && continue
    echo "unlisted upstream edit ($status): $path (add it to allowed_edits or revert it)"
  fi
  bad=1
done <"$changes"

if ((bad)); then exit 1; fi
echo "upstream drift OK: ${#allowed_edits[@]} allowed edits vs $(git rev-parse --short "$base")"

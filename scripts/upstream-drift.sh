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
  chipper/pkg/wirepod/ttr/kgsim.go       # CreateAIReq returns forkAIReq(...) (fork_prompt.go); c = forkClient(c) after the provider switch (fork_client.go); removeSpecialCharacters strips with forkMarkFilter() (fork_marks.go); stream sentence split = forkSplitSentence + EOF rep.dropCut / forkDropCut in kgsim_cmds.go (cap-cut sentence, from FinishReason) + forkFlushHeld (fork_split.go); Remember starts with forkCaptureChat (fork_memory.go); StreamingKGSim opens with forkBegin then skip = skip || forkPhotoTurn(req, ...) (phrase photo ask, fork_vision.go), stream EOF calls forkAfterReply, the speak loop ends in forkFollowUp (fork_followup.go); both BControl calls are defer forkBControl(..., esn)() — one answer per robot, forkEndKGLoop after the speak loop (fork_answer.go); stopStop is buffered (1) and the interrupter runs on forkInterruptBot with its istop (cuts the speech, then stop), its end + close(stopStop) deferred (fork_interrupt.go); the stream split skips rep.drop repeats, EOF ends silent on rep.only and wakes the speak loop with rep.wake, the speak loop takes each chunk from forkNextChunk (re-checks after every wake) (fork_followup.go); StreamingKGSim + KGSim dial via robotconn.Get (myvector/robotconn); the isKG wait goroutine is one forkWait(robot, forkWaitSearch, ...) call, a non-EOF stream error and a choice-less chunk call forkStreamFailed (ends the turn or the speak loop, and the wait; fork_wait.go)
  chipper/pkg/wirepod/ttr/kgsim_cmds.go  # same incl. forkClient and the stream split, on the LLM-commands/camera path; GetActionsFromString pads cmdPlusParam with "" ({{cmd}} panicked); DoSayText -> forkSayText (fork_say.go); DoGetImage defers forkCameraReq(&aireq, &c, robot.Cfg.SerialNo) — camera request + client swap (fork_prompt.go), its stream-create error calls forkVisionDown, the mirror/countdown/capture/shutter block is forkPhotoCapture (param "kept" = the kept photo, no capture; fork_vision.go), the capture's Data goes through forkPhotoData (failed capture → V8 line + stopImaging), its stream EOF marks the answer with forkPhotoAnswered before isDone (F6 listen) and calls forkPhotoWake and its speak loop takes each chunk from forkNextChunkUntil (lost-wakeup hang; ends on the stopped chan its stop ranger closes with stopImaging — interrupt mid-stream, fork_followup.go), its stream-EOF Remember is gated by !forkPhotoRemember (remembers (ask, answer) itself, so upstream's image-message Remember never runs; skips an interrupted turn), an empty reply returns at forkPhotoEmpty before fullRespSlice[0], a non-EOF stream error calls forkPhotoFailed (isDone + wake), a choice-less chunk is skipped before Choices[0], the speak loop ends in forkPhotoUnsaid (V8 line when nothing was spoken) and each chunk's actions pass forkPhotoActs (fork_vision.go)
  chipper/pkg/vars/vars.go               # LoadIntents returns forkFilterIntents(...) (fork_intents.go); reads intent-data/<ForkLanguage()> (fork_lang.go); GetRobot dials via robotconn.Get (myvector/robotconn)
  chipper/pkg/wirepod/ttr/matchIntentSend.go # ProcessTextAll partial pass: vars.ForkContainsPhrase (whole words); !locale = vars.ForkLanguage(); voiceText via vars.ForkNormalizeText; forkQuiet then forkListen first; IntentPass calls forkIntentPass once esn is set (memory episode via forkCaptureIntent, fork_memory.go; follow-up chain + photo listen end, fork_followup.go); whole file gofmt-ed 2026-11-10 (fork edits it often — expect whitespace conflicts on upstream merges)
  chipper/pkg/wirepod/localization/localization.go # GetText asks forkGetText first (fork_lang.go); whole file gofmt-ed 2026-11-10 (fork edits it often — expect whitespace conflicts on upstream merges)
  chipper/pkg/wirepod/ttr/words2num.go   # words2num input = forkTimerText(...) — et-EE timer forms (fork_timer.go); whole file gofmt-ed 2026-11-10 (fork edits it often — expect whitespace conflicts on upstream merges)
  chipper/pkg/wirepod/ttr/weather.go     # weatherParser apiLocation = forkWeatherLocation(...) — et-EE city word; getWeather returns forkGetWeather(...) first — weatherapi forecast + Open-Meteo race (fork_weather.go)
  chipper/pkg/wirepod/preqs/server.go    # sttHandler = forkMicCapture(forkSTTRouter(...)) — wake-turn start stamp + mic capture (fork_miccap.go) + et-EE STT router (fork_stt.go)
  chipper/webroot/index.html             # nav links to the fork pages /myvector/voice (myvector/voice), /myvector/prompt (myvector/prompt) and /myvector/wifi (myvector/wifi)
  chipper/pkg/initwirepod/startserver.go # non-android TLS pair = vars.CertPath/KeyPath (fork_paths.go: secrets/server/certs), was ../certs; EPConfig pair = forkEpodCert() (fork_epod.go)
  .dockerignore                          # + secrets, certs (never bake keys into an image)
  chipper/pkg/logger/logger.go           # LogUI/LogTray update the log globals under forkLogMu (fork_logger.go) — goroutines logged concurrently (race)
  chipper/pkg/wirepod/config-ws/webserver.go # get_logs/get_debug_logs read via logger.ForkLogList/ForkLogTrayList (fork_logger.go)
  chipper/pkg/wirepod/ttr/intentparam.go # names_username_extend face hint dials via robotconn.Get (myvector/robotconn), was vector.New per use
  setup.sh                               # certs dir ./certs -> ./secrets/server/certs (fork_paths.go)
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

#!/usr/bin/env bash
# Switch wire-pod's LLM ("Hey Vector, I have a question") between providers, and test it.
# Talks to the running server's web API, so switching needs no restart. Walkthrough: docs/llm-trials.md
#
#   scripts/llm.sh show                         current settings (key hidden)
#   scripts/llm.sh backup | restore FILE | off
#   scripts/llm.sh use PRESET [MODEL] [--[no-]commands] [--[no-]intentgraph] [--[no-]remember] [--prompt TEXT] [--voice NAME]
#        PRESET = ollama | claude | openai | together | gemini     (then runs `ping`)
#        commands/intentgraph/remember keep their saved value unless a flag is passed
#   scripts/llm.sh ping [QUESTION]              send a wire-pod-shaped request with the saved settings
#   scripts/llm.sh models [PRESET]              list model ids the provider offers
#   scripts/llm.sh warm [MODEL]                 load an Ollama model now (the first load takes ~1 min)
#   scripts/llm.sh ha-up | ha-down              Home Assistant container (Docker Desktop, port 8123)
#   scripts/llm.sh ha-agents                    list HA conversation agents (needs HA_TOKEN)
#   scripts/llm.sh ha-say [TEXT]                ask HA's agent (HA_AGENT) directly, like the plugin does
#   scripts/llm.sh ha-plugin [build|remove]     build/remove the NonaSuomy "assist ..." wire-pod plugin
#   scripts/llm.sh ha-plugin probe [OUT.so]     build it against a dead HA URL, outside chipper/plugins (live-check.sh ha)
#
# Keys come from the environment or ~/.config/my-vector/keys.env (outside the repo; LLM_KEYS overrides):
#   ANTHROPIC_API_KEY  OPENAI_API_KEY  TOGETHER_API_KEY  GEMINI_API_KEY  HA_TOKEN  [HA_URL] [HA_AGENT]
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WEB=${WIREPOD_WEB:-http://localhost:8080}
OLLAMA=${OLLAMA_URL:-http://localhost:11434}
WORK=$ROOT/robot/work/llm
KEYS=${LLM_KEYS:-$HOME/.config/my-vector/keys.env}
GO=/usr/local/go/bin/go
mkdir -p "$WORK"
if [[ -f $KEYS ]]; then set -a; . "$KEYS"; set +a; fi
HA_URL=${HA_URL:-http://localhost:8123}

die() { echo "error: $*" >&2; exit 1; }
need_key() { [[ -n ${!1:-} ]] || die "$1 is not set — add $1=... to $KEYS (chmod 600)"; }
get_kg() { curl -fsS -m 5 "$WEB/api/get_kg_api" || die "wire-pod web API not answering at $WEB — run: make start"; }
set_kg() { curl -fsS -m 5 -X POST "$WEB/api/set_kg_api" -H 'Content-Type: application/json' -d @- >/dev/null; }

show() {
  get_kg | python3 -I -c 'import json,sys
d=json.load(sys.stdin); d["key"]="<set>" if d.get("key") else ""
for k,v in d.items(): print(f"  {k:26} {v}")'
}

# preset -> provider, endpoint, key variable, default model
preset() {
  case $1 in
    ollama)   P=custom;   E=$OLLAMA/v1;                                          K=;                 M=qwen3.5:4b ;;
    claude)   P=custom;   E=https://api.anthropic.com/v1;                        K=ANTHROPIC_API_KEY; M=claude-haiku-5-5 ;;
    openai)   P=openai;   E=;                                                    K=OPENAI_API_KEY;    M=gpt-4o-mini ;;
    together) P=together; E=;                                                    K=TOGETHER_API_KEY;  M=meta-llama/Llama-3.3-70B-Instruct-Turbo ;;
    gemini)   P=custom;   E=https://generativelanguage.googleapis.com/v1beta/openai; K=GEMINI_API_KEY; M=gemini-2.5-flash ;;
    *) die "unknown preset '$1' (ollama|claude|openai|together|gemini)" ;;
  esac
}

use() {
  [[ $# -ge 1 ]] || die "usage: use PRESET [MODEL] [flags]"
  preset "$1"; shift
  if [[ $# -gt 0 && $1 != --* ]]; then M=$1; shift; fi
  # The three switches keep their saved value unless a flag sets them (9.5: a bare `use` once dropped intentgraph).
  local cmds ig rem prompt= voice= saved
  saved=$(get_kg | python3 -I -c 'import json,sys
d=json.load(sys.stdin); print(*(str(d.get(k) is True).lower() for k in ("commands_enable","intentgraph","save_chat")))')
  read -r cmds ig rem <<<"$saved"
  while [[ $# -gt 0 ]]; do
    case $1 in
      --commands) cmds=true ;; --intentgraph) ig=true ;; --remember) rem=true ;;
      --no-commands) cmds=false ;; --no-intentgraph) ig=false ;; --no-remember) rem=false ;;
      --prompt) prompt=${2?}; shift ;; --voice) voice=${2?}; shift ;;
      *) die "unknown flag $1" ;;
    esac; shift
  done
  # OpenAI TTS is called with the provider's key, so any other provider leaves Vector silent.
  if [[ -n $voice ]]; then
    [[ $P == openai ]] || die "--voice works only with the openai preset"
    [[ " alloy onyx fable shimmer nova echo " == *" $voice "* ]] || die "--voice: one of alloy onyx fable shimmer nova echo"
  fi
  # ping always omits top_p for Claude, so it can't tell whether the server runs fork_llm.go yet.
  local started fix=$ROOT/chipper/pkg/wirepod/ttr/fork_llm.go
  started=$(systemctl show wire-pod -p ActiveEnterTimestamp --value 2>/dev/null || true)
  if [[ ${M,,} == claude* && -n $started ]] && (($(date -d "$started" +%s) < $(stat -c %Y "$fix"))); then
    echo "WARNING: wire-pod started before the Claude fix — run: make start (ping below still passes)" >&2
  fi
  local key=ollama
  if [[ -n $K ]]; then need_key "$K"; key=${!K}; fi
  # Full settings every time, so nothing from the previous provider leaks through.
  P=$P E=$E M=$M KEY=$key CMDS=$cmds IG=$ig REM=$rem PROMPT=$prompt VOICE=$voice python3 -I -c 'import json,os
e=os.environ; b=lambda k: e[k]=="true"
print(json.dumps({"enable":True,"provider":e["P"],"endpoint":e["E"],"model":e["M"],"key":e["KEY"],"id":"",
 "robotName":"Vector","openai_prompt":e["PROMPT"],"openai_voice":e["VOICE"],"openai_voice_with_english":bool(e["VOICE"]),
 "commands_enable":b("CMDS"),"intentgraph":b("IG"),"save_chat":b("REM"),"temp":1,"top_p":1}))' | set_kg
  echo "saved: provider=$P model=$M commands=$cmds intentgraph=$ig remember=$rem voice=${voice:-robot}"
  ping
}

# Smoke test of kgsim.go's request: same endpoint, key, model, stream, temperature, top_p and reasoning rule;
# no command prompt, chat history or 2048-token cap. Key goes through the environment, never argv.
ping() {
  local q=${1:-Why is the sky blue? Answer in one sentence.}
  get_kg | Q=$q python3 -I -c 'import json,os,sys,time,urllib.parse,urllib.request
c=json.load(sys.stdin)
if not c.get("enable"): sys.exit("LLM is off — run: scripts/llm.sh use PRESET")
base={"openai":"https://api.openai.com/v1","together":"https://api.together.xyz/v1"}.get(c["provider"],c.get("endpoint","")).rstrip("/")
model=c["model"] or "gpt-4o-mini"
sysmsg=c.get("openai_prompt") or "You are a helpful, animated robot called Vector. Keep the response concise yet informative."
body={"model":model,"stream":True,"temperature":1,"max_completion_tokens":300,
      "messages":[{"role":"system","content":sysmsg},{"role":"user","content":os.environ["Q"]}]}
if not model.lower().startswith("claude"): body["top_p"]=1
if c["provider"]=="custom" and urllib.parse.urlparse(base).port==11434:  # forkAIReq/forkReplyCap (fork_prompt.go)
    body["reasoning_effort"]="none"; body["max_tokens"]=body.pop("max_completion_tokens")
req=urllib.request.Request(base+"/chat/completions",json.dumps(body).encode(),
    {"Content-Type":"application/json","Authorization":"Bearer "+c.get("key","")})
print("->", c["provider"], base, "model="+model)
t0=time.time(); first=None; out=[]
try:
    with urllib.request.urlopen(req,timeout=120) as r:
        for line in r:
            line=line.decode().strip()
            if not line.startswith("data:") or line.endswith("[DONE]"): continue
            ch=json.loads(line[5:]).get("choices") or [{}]
            txt=(ch[0].get("delta") or {}).get("content") or ""
            if txt and first is None: first=time.time()-t0
            out.append(txt)
except urllib.error.HTTPError as e:
    sys.exit(f"HTTP {e.code}: {e.read().decode()[:400]}")
except Exception as e:
    sys.exit(f"failed: {e}")
print("<-", "".join(out).strip() or "(empty reply)")
ttfb="%.2fs" % first if first is not None else "-"
print("first words after", ttfb + ", done after %.2fs" % (time.time()-t0))'
}

models() {
  local p=${1:-}
  if [[ -z $p ]]; then
    p=$(get_kg | python3 -I -c 'import json,sys;c=json.load(sys.stdin);e=c.get("endpoint","")
print({"openai":"openai","together":"together"}.get(c["provider"]) or ("claude" if "anthropic" in e else "gemini" if "googleapis" in e else "ollama"))')
  fi
  case $p in
    ollama)   curl -fsS "$OLLAMA/api/tags" | python3 -I -c 'import json,sys;[print(m["name"]) for m in json.load(sys.stdin)["models"]]' ;;
    claude)   need_key ANTHROPIC_API_KEY
              curl -fsS https://api.anthropic.com/v1/models -H "x-api-key: $ANTHROPIC_API_KEY" -H 'anthropic-version: 2023-06-01' \
                | python3 -I -c 'import json,sys;[print(m["id"]) for m in json.load(sys.stdin)["data"]]' ;;
    openai)   need_key OPENAI_API_KEY
              curl -fsS https://api.openai.com/v1/models -H "Authorization: Bearer $OPENAI_API_KEY" \
                | python3 -I -c 'import json,sys;[print(m["id"]) for m in sorted(json.load(sys.stdin)["data"],key=lambda m:m["id"])]' ;;
    together) need_key TOGETHER_API_KEY
              curl -fsS https://api.together.xyz/v1/models -H "Authorization: Bearer $TOGETHER_API_KEY" \
                | python3 -I -c 'import json,sys;[print(m["id"]) for m in json.load(sys.stdin) if m.get("type")=="chat"]' ;;
    gemini)   need_key GEMINI_API_KEY
              curl -fsS https://generativelanguage.googleapis.com/v1beta/openai/models -H "Authorization: Bearer $GEMINI_API_KEY" \
                | python3 -I -c 'import json,sys;[print(m["id"]) for m in json.load(sys.stdin)["data"]]' ;;
    *) die "unknown preset '$p'" ;;
  esac
}

warm() {
  local m=${1:-qwen3.5:4b}
  echo "loading $m into Ollama (first time ~1 min)..."
  curl -fsS -m 300 "$OLLAMA/api/generate" -d "{\"model\":\"$m\",\"keep_alive\":-1}" >/dev/null
  curl -fsS "$OLLAMA/api/ps" | python3 -I -c 'import json,sys;[print(" loaded:",m["name"],"until",m["expires_at"][:10]) for m in json.load(sys.stdin)["models"]]'
}

ha_up() {
  docker inspect homeassistant >/dev/null 2>&1 && { docker start homeassistant >/dev/null; echo "Home Assistant started: $HA_URL"; return; }
  docker run -d --name homeassistant --restart unless-stopped -p 8123:8123 \
    -e TZ="$(timedatectl show -p Timezone --value 2>/dev/null || echo UTC)" \
    -v "$WORK/ha-config:/config" ghcr.io/home-assistant/home-assistant:stable >/dev/null
  echo "Home Assistant starting (first boot ~1-2 min): $HA_URL — config in $WORK/ha-config"
}

ha_agents() {
  need_key HA_TOKEN
  curl -fsS -m 10 "$HA_URL/api/states" -H "Authorization: Bearer $HA_TOKEN" \
    | python3 -I -c 'import json,sys;[print(" ",s["entity_id"]," — ",s["attributes"].get("friendly_name","")) for s in json.load(sys.stdin) if s["entity_id"].startswith("conversation.")]'
}

# Sends Q to HA's conversation API the same way the plugin does; prints HA's spoken reply.
ha_say() {
  need_key HA_TOKEN
  Q=${1:-Hello, who are you?} A=${HA_AGENT:-} python3 -I -c 'import json,os
b={"text":os.environ["Q"]}
if os.environ["A"]: b["agent_id"]=os.environ["A"]
print(json.dumps(b))' | curl -fsS -m 60 "$HA_URL/api/conversation/process" -H "Authorization: Bearer $HA_TOKEN" \
      -H 'Content-Type: application/json' -d @- \
    | python3 -I -c 'import json,sys;print("<-",json.load(sys.stdin)["response"]["speech"]["plain"]["speech"])'
}

ha_plugin() {
  local so=$ROOT/chipper/plugins/commandha.so src=$WORK/ha-plugin-src build=$WORK/ha-plugin mode=${1:-build}
  if [[ $mode == remove ]]; then
    rm -f "$so"; echo "removed $so — run: make start"; return
  fi
  if [[ $mode == probe ]]; then # HA down on purpose: a dead URL, dummy token/agent, built outside chipper/plugins
    so=${2:-$ROOT/robot/work/live-check/commandha-dead.so} build=$WORK/ha-plugin-probe
    local HA_URL=http://127.0.0.1:9 HA_TOKEN=probe HA_AGENT=probe
  else
    need_key HA_TOKEN
    [[ -n ${HA_AGENT:-} ]] || die "set HA_AGENT (pick one from: scripts/llm.sh ha-agents) — without it HA uses its non-LLM default agent"
  fi
  [[ -d $src ]] || git clone -q --depth 1 https://github.com/NonaSuomy/WirePodVectorCommandHomeAssistant "$src"
  echo "plugin source $(git -C "$src" log --oneline -1)"
  [[ $mode == probe ]] || ha_say "Hello" >/dev/null || die "HA did not answer with HA_TOKEN/HA_AGENT — fix that first (scripts/llm.sh ha-agents)"
  mkdir -p "$build" "$(dirname "$so")"
  # Fill the placeholders the upstream file asks you to edit by hand; switch on its agent_id lines.
  sed -e "s#http://HAIPHERE:8123#$HA_URL#" -e "s#LongLiveTokenHere#$HA_TOKEN#" \
      -e "s#//AgentID string#AgentID string#" -e "s#//agentID := \"AgentIDHere\"#agentID := \"$HA_AGENT\"#" \
      -e "s#//AgentID: agentID,#AgentID: agentID,#" \
      -e 's#panic(err)#return "intent_imperative_praise", "Home Assistant is not reachable."#' "$src/commandha.go" >"$build/commandha.go"
  chmod 600 "$build/commandha.go"
  grep -q 'agentID := "' "$build/commandha.go" || die "agent_id substitution did not apply — upstream file changed"
  ! grep -q 'panic(' "$build/commandha.go" || die "panic patch did not apply — upstream file changed (an unreachable HA would restart wire-pod)"
  (cd "$build" && CGO_ENABLED=1 "$GO" build -buildmode=plugin -o "$so" commandha.go)
  chmod 600 "$so"
  [[ $mode == probe ]] && { echo "built $so"; return; }
  echo "built $so — run: make start, then check the log for: Action func in plugin commandha.so is OK"
}

case ${1:-} in
  show) show ;;
  backup) f=$WORK/kg-$(date +%Y%m%d-%H%M%S).json
          (umask 077; get_kg >"$f.tmp") || { rm -f "$f.tmp"; exit 1; }; mv "$f.tmp" "$f"; echo "saved $f" ;;
  restore) [[ -f ${2:-} ]] || die "usage: restore FILE"; set_kg <"$2"; echo restored; show ;;
  off) echo '{"enable":false}' | set_kg; echo "LLM off" ;;
  use) shift; use "$@" ;;
  ping) shift; ping "$@" ;;
  models) shift; models "$@" ;;
  warm) shift; warm "$@" ;;
  ha-up) ha_up ;;
  ha-down) docker rm -f homeassistant >/dev/null && echo "Home Assistant removed (config kept in $WORK/ha-config)" ;;
  ha-agents) ha_agents ;;
  ha-say) shift; ha_say "$@" ;;
  ha-plugin) shift; ha_plugin "$@" ;;
  *) sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 1 ;;
esac

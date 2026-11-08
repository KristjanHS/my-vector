#!/usr/bin/env bash
# Host test of the robot sound lock (make check): env.sh's guard, the sound scripts, quiet.sh — fake robot.env, fake curl/ssh.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/quiet-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: quiet guard test: $*" >&2; exit 1; }

mkdir -p "$tmp/bin"
for c in curl ssh scp; do printf '#!/bin/sh\necho "%s $*" >>"%s/calls"\nexit 7\n' "$c" "$tmp" >"$tmp/bin/$c"; chmod +x "$tmp/bin/$c"; done
: >"$tmp/key"
printf 'ROBOT_ESN=000fake\nROBOT_IP=192.0.2.1\nROBOT_SERVER=192.0.2.2\nROBOT_KEY=%s\n' "$tmp/key" >"$tmp/robot.env"
export PATH="$tmp/bin:$PATH" ROBOT_ENV="$tmp/robot.env" MYVECTOR_QUIET_FILE="$tmp/quiet-until" MYVECTOR_URL=http://127.0.0.1:9
lock() { echo "$(($(date +%s) + $1)) ${2:--} voice:shut up" >"$MYVECTOR_QUIET_FILE"; }
printf '. "%s/env.sh"\necho PAST\n' "$robot" >"$tmp/sound.sh"
printf 'silent=1\n. "%s/env.sh"\necho PAST\n' "$robot" >"$tmp/silent.sh"

lock 600 4
rc=0; out=$(bash "$tmp/sound.sh" 2>&1) || rc=$?
((rc == 3)) || die "sound script under an active lock: want exit 3, got $rc: $out"
[[ $out == "QUIET: robot sound locked until "*" (voice:shut up); make unquiet lifts it" ]] || die "QUIET line: $out"
out=$(bash "$tmp/silent.sh" 2>&1) || die "silent=1 script blocked: $out"
lock -5
out=$(bash "$tmp/sound.sh" 2>&1) || die "expired lock still blocks: $out"
echo garbage >"$MYVECTOR_QUIET_FILE"
out=$(bash "$tmp/sound.sh" 2>&1) || die "garbage lock file blocks: $out"
rm -f "$MYVECTOR_QUIET_FILE"
out=$(bash "$tmp/sound.sh" 2>&1) || die "no lock file blocks: $out"

# Every sound script stops at the guard: exit 3, the QUIET line, no robot call.
lock 600
sound=(voice-samples/onset.sh voice-samples/play.sh volume-probe/sweep.sh volume-probe/idle.sh volume-probe/anim.sh
  volume-probe/pcvoice.sh unlock/power-stress.sh openmic/probe.sh)
for s in "${sound[@]}"; do
  rc=0; out=$("$robot/$s" et-EE x 2>&1) || rc=$?
  ((rc == 3)) || die "$s under a lock: want exit 3, got $rc: $out"
  [[ $out == *QUIET:* ]] || die "$s: no QUIET line: $out"
done
[[ ! -s $tmp/calls ]] || die "a blocked script called the robot: $(cat "$tmp/calls")"

# G3: a lock set mid-test stops the loop — the fake curl locks on sweep.sh's first sound; exactly one sound plays.
rm -f "$MYVECTOR_QUIET_FILE"
mkdir -p "$tmp/bin3"
cat >"$tmp/bin3/curl" <<F
#!/bin/sh
case "\$*" in
  *get_sdk_info*) echo '{"esn":"000fake"}' ;;
  *PostAudioEvent*) echo sound >>"$tmp/sounds"; echo "\$((\$(date +%s) + 600)) - voice:shut up" >"$MYVECTOR_QUIET_FILE" ;;
esac
F
chmod +x "$tmp/bin3/curl"
rc=0; out=$(PATH="$tmp/bin3:$PATH" CONDS=base:1.0 SOUNDS="chime blink curious" OUT=$tmp/out "$robot/volume-probe/sweep.sh" 2>&1) || rc=$?
((rc == 3)) && [[ $out == *QUIET:* ]] || die "sweep.sh with a mid-run lock: want exit 3 + QUIET, got $rc: $out"
[[ $(wc -l <"$tmp/sounds") -eq 1 ]] || die "sweep.sh played $(wc -l <"$tmp/sounds") sounds after the lock, want 1"

# Static: every robot script reaching env.sh is a listed sound script or sets silent=1 (a new unlisted script fails here).
srcre='^[[:space:]]*(\[\[[^]]*\]\][[:space:]]*\|\|[[:space:]]*)?\.[[:space:]]+".*'
reaches() { # reaches <file>: it sources env.sh, or a lib.sh (own dir or ../<dir>/lib.sh) that does
  grep -qE "$srcre/env\.sh\"" "$1" && return 0
  local lib
  for lib in $(grep -oE "$srcre/lib\.sh\"" "$1" | grep -oE '(\.\./[a-z-]+/)?lib\.sh"$' | tr -d '"'); do
    lib=$(dirname "$1")/$lib
    [[ -f $lib ]] && grep -qE "$srcre/env\.sh\"" "$lib" && return 0
  done
  return 1
}
unlisted=()
for f in "$robot"/*/*.sh "$robot"/*/tests/*.sh; do
  rel=${f#"$robot"/}
  [[ $rel == */lib.sh || $rel == quiet/* || $rel == env.sh ]] && continue
  reaches "$f" || continue
  [[ " ${sound[*]} " == *" $rel "* ]] && continue
  grep -qE '^silent=1( |$)' "$f" || unlisted+=("$rel")
done
((${#unlisted[@]} == 0)) || die "sources env.sh but neither a listed sound script nor silent=1: ${unlisted[*]}"

# quiet.sh with wire-pod down: on writes the lock file, never shortens it, status exits 3, off removes it.
rm -f "$MYVECTOR_QUIET_FILE"
"$robot/quiet/quiet.sh" status >/dev/null || die "status unlocked: want exit 0"
"$robot/quiet/quiet.sh" on 5 >/dev/null 2>&1 || die "on 5 failed"
read -r u v src <"$MYVECTOR_QUIET_FILE"
((u > $(date +%s) + 290)) && [[ $v == - && $src == cli:quiet.sh ]] || die "on 5 wrote: $(cat "$MYVECTOR_QUIET_FILE")"
"$robot/quiet/quiet.sh" on 1 >/dev/null 2>&1
read -r u2 _ <"$MYVECTOR_QUIET_FILE"
((u2 == u)) || die "on 1 shortened the lock"
rc=0; "$robot/quiet/quiet.sh" status >/dev/null || rc=$?
((rc == 3)) || die "status locked: want exit 3, got $rc"
"$robot/quiet/quiet.sh" off >/dev/null 2>&1
[[ ! -e $MYVECTOR_QUIET_FILE ]] || die "off left the lock file"
echo "quiet guard host test OK"

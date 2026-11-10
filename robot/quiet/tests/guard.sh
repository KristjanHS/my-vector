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
rc=0; out=$(silent=1 bash "$tmp/sound.sh" 2>&1) || rc=$?
((rc == 3)) || die "an exported silent=1 skipped the lock: want exit 3, got $rc: $out"
lock -5
out=$(bash "$tmp/sound.sh" 2>&1) || die "expired lock still blocks: $out"
echo garbage >"$MYVECTOR_QUIET_FILE"
out=$(bash "$tmp/sound.sh" 2>&1) || die "garbage lock file blocks: $out"
rm -f "$MYVECTOR_QUIET_FILE"
out=$(bash "$tmp/sound.sh" 2>&1) || die "no lock file blocks: $out"

# Every sound script stops at the guard: exit 3, the QUIET line, no robot call.
lock 600
sound=(voice-samples/onset.sh voice-samples/play.sh volume-probe/sweep.sh volume-probe/idle.sh volume-probe/anim.sh volume-probe/loopstop.sh
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
# I4: onset.sh and pcvoice.sh T2 re-check per run — the fake curl locks on the first console SayText; exactly one plays.
mkdir -p "$tmp/bin4"
cat >"$tmp/bin4/curl" <<F
#!/bin/sh
case "\$*" in
  *get_sdk_info*) echo '{"esn":"000fake"}' ;;
  *func=SayText*) echo say >>"$tmp/says"; echo "\$((\$(date +%s) + 600)) - voice:shut up" >"$MYVECTOR_QUIET_FILE" ;;
esac
F
cat >"$tmp/bin4/ssh" <<'F'
#!/bin/sh
case "$*" in *'cat /data/data/myvector-voice.json') echo '{"url": "http://192.0.2.2:8091", "slow": 1.0}' ;; *'wc -l'*) echo 0 ;; esac
F
printf '#!/bin/sh\n' >"$tmp/bin4/sleep"
chmod +x "$tmp/bin4/"*
for s in voice-samples/onset.sh volume-probe/pcvoice.sh; do
  rm -f "$MYVECTOR_QUIET_FILE" "$tmp/says"
  rc=0; out=$(PATH="$tmp/bin4:$PATH" RUNS=3 OUT=$tmp/out4 "$robot/$s" stock 2>&1) || rc=$?
  ((rc == 3)) && [[ $out == *QUIET:* ]] || die "$s with a mid-run lock: want exit 3 + QUIET, got $rc: $out"
  [[ $(wc -l <"$tmp/says") -eq 1 ]] || die "$s played $(wc -l <"$tmp/says") SayText lines after the lock, want 1"
done

# C1: under a lock restarts.sh, catch.sh and epod/apply.sh write Q1's silent-start flag before the anki-robot.target restart;
# unlocked they write none. A symlinked tree keeps their robot/work output in $tmp.
t5=$tmp/tree; bin=$t5/work/wire-os-victor/_build/vicos/Release/bin
mkdir -p "$t5/quiet" "$t5/crash-review" "$t5/seassert" "$bin" "$tmp/bin5" "$tmp/epod"
for f in env.sh quiet/guard.sh crash-review/lib.sh crash-review/restarts.sh seassert/catch.sh; do ln -s "$robot/$f" "$t5/$f"; done
echo stripped >"$bin/vic-anim"; echo full >"$bin/vic-anim.full"
printf -- '-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----\n' >"$tmp/epod/ca.crt"
cat >"$tmp/bin5/ssh" <<F
#!/bin/sh
for a; do c=\$a; done
echo "ssh \$c" >>"$tmp/calls5"
case "\$*" in
  *silent-start*) ;;
  *'sh -s') cat >/dev/null; echo 'epod: wrote' ;;
  *fault-code.service*) exit 3 ;;
  *pidof*) exit 1 ;;
  *md5sum*) echo '$(md5sum <"$bin/vic-anim" | cut -d' ' -f1)  /anki/bin/vic-anim' ;;
  *'date +%s'*) date +%s ;;
esac
F
cat >"$tmp/bin5/gdb-multiarch" <<'F'
#!/bin/sh
case "$*" in *disas*) printf '   0x00001000 <+0>:\tldr\tr0, [pc]\n   0x00001004 <+4>:\tmov\tr3, #191\n' ;; esac
F
printf '#!/bin/sh\n' >"$tmp/bin5/sleep"
chmod +x "$tmp/bin5/"*
restarters=("$t5/crash-review/restarts.sh 1" "$t5/seassert/catch.sh 1" "$robot/epod/apply.sh")
for locked in 1 0; do
  if ((locked)); then lock 600; else rm -f "$MYVECTOR_QUIET_FILE"; fi
  for s in "${restarters[@]}"; do
    : >"$tmp/calls5"
    # shellcheck disable=SC2086 # s = script + args
    rc=0; out=$(PATH="$tmp/bin5:$PATH" MYVECTOR_EPOD_DIR=$tmp/epod $s 2>&1 </dev/null) || rc=$?
    w=$(grep -n silent-start "$tmp/calls5" | head -1 | cut -d: -f1 || true)
    r=$(grep -nE 'systemctl (stop|restart) anki-robot\.target|sh -s$' "$tmp/calls5" | head -1 | cut -d: -f1 || true)
    [[ -n $r ]] || die "${s#"$tmp"/} made no restart call (rc $rc): $out"
    if ((locked)); then
      { [[ -n $w ]] && ((w < r)); } || die "${s#"$tmp"/} under a lock: no silent-start write before the restart: $(head -5 "$tmp/calls5")"
    else
      [[ -z $w ]] || die "${s#"$tmp"/} unlocked wrote the silent-start flag: $(sed -n "${w}p" "$tmp/calls5")"
    fi
  done
done

# Static: every robot/**/*.sh reaching env.sh (`.`/`source` of env.sh, $envsh, or a lib.sh that does) is a listed sound
# script or sets silent=1 before its first such line; fixtures below prove each form is caught.
srcre='(^|[;&|({[:space:]])(\.|source)[[:space:]]+[^#]*'
src_lines() { grep -nE "$srcre$2" "$1" | grep -vE '^[0-9]+:[[:space:]]*#' | cut -d: -f1 || true; }
first_src() { # first_src <file>: line number of its first line reaching env.sh, empty when none
  local lib n p out
  out=$({ src_lines "$1" '(env\.sh|envsh)'
    for n in $(src_lines "$1" 'lib\.sh'); do
      p=$(grep -oE '(\.\./[A-Za-z0-9_-]+/)?lib\.sh' <<<"$(sed -n "${n}p" "$1")"); p=${p%%$'\n'*}
      lib=$(dirname "$1")/$p
      [[ -n $p && -f $lib && -z $(src_lines "$lib" '(env\.sh|envsh)') ]] || echo "$n" # an unresolvable lib.sh counts as reaching
    done; } | sort -n); echo "${out%%$'\n'*}"
}
unlisted_in() { # unlisted_in <root>: the scripts under <root> failing the check, root-relative
  local f rel first s
  while IFS= read -r f; do
    rel=${f#"$1"/}
    [[ $rel == */lib.sh || $rel == env.sh ]] && continue
    first=$(first_src "$f"); [[ -n $first ]] || continue
    [[ " ${sound[*]} " == *" $rel "* ]] && continue
    s=$(grep -nE '^[[:space:]]*silent=1([[:space:]]|;|$)' "$f"); s=${s%%$'\n'*} s=${s%%:*}
    [[ -n $s ]] && ((s < first)) || echo "$rel"
  done < <(find "$1" \( -path "$1/work" -o -path "$1/ota" -o -path "$1/backup" -o -path "$1/quiet" \) -prune -o -name '*.sh' -type f -print)
}
fx=$tmp/fx; mkdir -p "$fx/a/deep" "$fx/b" "$fx/lib"
# shellcheck disable=SC2016 # fixture scripts: literal $
{
printf 'source "$x/env.sh"\n' >"$fx/a/deep/source-form.sh"
printf 'envsh=$h/robot/env.sh\n. "$envsh"\n' >"$fx/a/envsh-form.sh"
printf '. "$h/../env.sh"\nsilent=1\n' >"$fx/a/silent-after.sh"
printf '. "$h/../lib/lib.sh"\n' >"$fx/b/via-lib.sh"; printf '. "$d/../env.sh"\n' >"$fx/lib/lib.sh"
printf 'silent=1 # x\n[[ -n $y ]] || . "$h/../env.sh"\n' >"$fx/b/ok.sh"
printf '# . "$h/../env.sh"\n. "$h/guard.sh" # no env.sh\n' >"$fx/b/comment-only.sh"
}
got=$(unlisted_in "$fx" | sort | tr '\n' ' ')
[[ $got == "a/deep/source-form.sh a/envsh-form.sh a/silent-after.sh b/via-lib.sh " ]] || die "static check fixtures: flagged '$got'"
unlisted=$(unlisted_in "$robot" | tr '\n' ' ')
[[ -z $unlisted ]] || die "sources env.sh but neither a listed sound script nor silent=1 before it: $unlisted"

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
# I3: on keeps a saved volume (a live shorter lock's, an expired file's); off with one expires the file, prints it.
for d in 60 -60; do
  lock "$d" 4
  "$robot/quiet/quiet.sh" on 5 >/dev/null 2>&1 || die "on 5 over a lock with volume 4 failed"
  read -r u v src <"$MYVECTOR_QUIET_FILE"
  ((u > $(date +%s) + 290)) && [[ $v == 4 && $src == cli:quiet.sh ]] || die "on 5 over '$d s, vol 4' wrote: $(cat "$MYVECTOR_QUIET_FILE")"
done
out=$("$robot/quiet/quiet.sh" off 2>&1)
[[ $out == *"volume back to 4"* ]] || die "off WARN lacks the saved volume: $out"
[[ -f $MYVECTOR_QUIET_FILE ]] || die "off with a saved volume removed the lock file"
read -r u v _ <"$MYVECTOR_QUIET_FILE"
((u <= $(date +%s))) && [[ $v == 4 ]] || die "off with a saved volume: want an expired file keeping 4, got $(cat "$MYVECTOR_QUIET_FILE")"
"$robot/quiet/quiet.sh" status >/dev/null || die "status after off: want unlocked"
# I-b: wire-pod answering off with 409 (a newer lock kept): no fallback, the file stays, off prints the QUIET line.
mkdir -p "$tmp/bin409"
printf '#!/bin/sh\nprintf 409\n' >"$tmp/bin409/curl"; chmod +x "$tmp/bin409/curl"
lock 600 4
out=$(PATH="$tmp/bin409:$PATH" "$robot/quiet/quiet.sh" off 2>&1)
[[ $out == "QUIET: robot sound locked until "*" (voice:shut up); make unquiet lifts it" ]] || die "off answered 409: want the QUIET line, got: $out"
read -r u v _ <"$MYVECTOR_QUIET_FILE"
((u > $(date +%s) + 590)) && [[ $v == 4 ]] || die "off answered 409 changed the lock: $(cat "$MYVECTOR_QUIET_FILE")"
echo "quiet guard host test OK"

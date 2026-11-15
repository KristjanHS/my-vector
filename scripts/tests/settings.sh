#!/usr/bin/env bash
# scripts/lib/settings.sh layer precedence (defaults < dotfiles < companion < robot.env) and robot/env.sh's --no-defaults SETUP.
set -euo pipefail
dir=$(cd "$(dirname "$0")/../.." && pwd)
t=$(mktemp -d); trap 'rm -rf "$t"' EXIT
mkdir -p "$t/dot/shared-settings" "$t/priv"
printf '# c\nPC_LAN_IP=10.0.0.1\nPC_TAILNET_IP=10.0.0.4\nROBOT_IP=10.0.0.2\n' >"$t/dot/shared-settings/settings.env"
printf 'ROBOT_IP=10.0.0.3\nROBOT_ESN=0000abcd\nROBOT_SERVER=http://localhost:1\n' >"$t/priv/settings.env"
touch "$t/key"; printf 'ROBOT_KEY=%s\n' "$t/key" >"$t/robot.env"
nbad=0; fail() { echo "FAIL settings: $*" >&2; nbad=$((nbad + 1)); } # report every failing case, exit 1 at the end
export DOTFILES_DIR=$t/dot MY_VECTOR_OVERLAY_DIR=$t/priv
dump=$(bash "$dir/scripts/lib/settings.sh" --dump)
for want in PC_LAN_IP=10.0.0.1 ROBOT_IP=10.0.0.3 ROBOT_ESN=0000abcd PHONE_TAILNET_IP=192.0.2.53; do
  grep -qx "$want" <<<"$dump" || fail "--dump lacks $want: $dump"
done
nd=$(bash "$dir/scripts/lib/settings.sh" --dump --no-defaults)
grep -qx ROBOT_IP=10.0.0.3 <<<"$nd" || fail "--no-defaults dropped the companion layer: $nd"
if grep -q '^PHONE_TAILNET_IP=' <<<"$nd"; then fail "--no-defaults leaked a public default"; fi
rc=0; bash "$dir/scripts/lib/settings.sh" --dump --no-default >/dev/null 2>&1 || rc=$?
[[ $rc == 2 ]] || fail "settings.sh accepted a mistyped --no-defaults (rc=$rc)"
grep -qx 'define ROBOT_IP = 10.0.0.3' <<<"$("$dir/scripts/guard-nft.sh" </dev/null)" || fail "guard-nft.sh ignores the companion layer"
# shellcheck disable=SC2016 # the inner bash expands them
envsh() { env -u ESN -u SERVER -u ROBOT_TTS_URL -u ROBOT_IP -u MYVECTOR_EPOD_DIR ROBOT_ENV="$1" bash -c 'silent=1; . "$0/robot/env.sh"; echo "$ip $esn $tts_url"' "$dir"; }
[[ $(envsh "$t/robot.env") == "10.0.0.3 0000abcd http://10.0.0.4:8091" ]] || fail "env.sh layering: $(envsh "$t/robot.env" 2>&1)"
echo ROBOT_IP=10.0.0.9 >>"$t/robot.env"
[[ $(envsh "$t/robot.env") == "10.0.0.9 "* ]] || fail "robot.env does not override the settings layers"
export DOTFILES_DIR=/nonexistent MY_VECTOR_OVERLAY_DIR=/nonexistent
printf 'ROBOT_KEY=%s\nROBOT_ESN=0000abcd\nROBOT_SERVER=http://localhost:1\n' "$t/key" >"$t/bare.env" # all but ROBOT_IP
rc=0; out=$(envsh "$t/bare.env" 2>&1) || rc=$?
[[ $rc == 2 && $out == SETUP:* ]] || fail "no layers: want SETUP exit 2 (public placeholders must not reach env.sh), got rc=$rc: $out"
# Parser contract (same as hands-on-llm's reader): a layer is parsed, never executed. One layer per case, alone via --dump.
mkdir -p "$t/case/shared-settings"; cf=$t/case/shared-settings/settings.env
load() { # load <file content>: sets rc, out (the dump), err (stderr)
  printf '%s' "$1" >"$cf"
  rc=0; out=$(DOTFILES_DIR=$t/case bash "$dir/scripts/lib/settings.sh" --dump --no-defaults 2>"$t/err") || rc=$?
  err=$(<"$t/err")
}
reject() { # reject <file content> <what> [lineno]: non-zero, and stderr is only the invalid-line message (never the value)
  load "$1"
  [[ $rc != 0 ]] || { fail "accepted $2: $out"; return; }
  [[ $err == "settings: $cf:${3:-1}: invalid line" ]] || fail "$2: want only the invalid-line message, got: $err"
}
load $'# c\n\nK=$(>'"$t"$'/pwned)\n'
# shellcheck disable=SC2016 # literal layer text, never expanded
[[ ! -e $t/pwned ]] || fail 'a layer line was executed: K=$(>marker) created the marker'
[[ $rc != 0 && $err == "settings: $cf:3: invalid line" ]] || fail "K=\$(>marker) on line 3: rc=$rc err=$err"
# shellcheck disable=SC2016 # literal layer text, never expanded
reject $'K=$(id)\n' 'K=$(id)'
reject $'A=1 \n' 'a trailing space'
reject $'export A=1\n' 'export A=1'
reject $'A="v"\n' 'A="v"'
reject $'A=\'v\'\n' "A='v'"
reject $'A=v # c\n' 'an inline comment'
reject $'A=v#c\n' 'a # in a value'
reject $'A=`id`\n' 'a backtick'
reject $'A=a\\b\n' 'a backslash'
reject $'\xc3\x84=1\n' 'a non-ASCII key'
load $'A=1\r\nB=2\r\n'
[[ $rc == 0 && $out == *$'A=1\nB=2'* && $out != *$'\r'* ]] || fail "CRLF layer: rc=$rc out=$(cat -A <<<"$out")"
load $'\xef\xbb\xbfA=1\nB=2\n'
[[ $rc == 0 && $out == *$'A=1\nB=2'* ]] || fail "BOM layer lost its first key: rc=$rc out=$out err=$err"
load $'A=1\nA=2\n'
[[ $rc == 0 && $out == A=2 ]] || fail "last occurrence must win: rc=$rc out=$out"
load 'A=1'
[[ $rc == 0 && $out == A=1 ]] || fail "a last line without newline was dropped: rc=$rc out=$out"
# An invalid later layer exports nothing (no half-loaded environment), and env.sh fails closed on it.
mkdir -p "$t/okdot/shared-settings"; echo A=new >"$t/okdot/shared-settings/settings.env"; printf 'A=1\nB=x y\n' >"$t/priv/settings.env"
# shellcheck disable=SC2016 # the inner bash expands them
got=$(DOTFILES_DIR=$t/okdot MY_VECTOR_OVERLAY_DIR=$t/priv bash -c '. "$0/scripts/lib/settings.sh"; A=orig; settings_load --no-defaults; echo "$? $A"' "$dir" 2>/dev/null)
[[ $got == "1 orig" ]] || fail "an invalid layer half-loaded the environment: want '1 orig', got '$got'"
printf 'ROBOT_KEY=%s\nROBOT_ESN=0000abcd\nROBOT_SERVER=http://localhost:1\nROBOT_IP=10.0.0.9\n' "$t/key" >"$t/full.env"
rc=0; out=$(DOTFILES_DIR=$t/okdot MY_VECTOR_OVERLAY_DIR=$t/priv envsh "$t/full.env" 2>&1) || rc=$?
[[ $rc == 2 && $out == "settings: $t/priv/settings.env:2: invalid line" ]] || fail "env.sh must exit 2 on an invalid layer, got rc=$rc: $out"
((nbad == 0)) || exit 1
echo "settings OK: layer precedence, robot.env override, env.sh skips public defaults, parser contract (CRLF/BOM/rejects/last-wins/no exec)"

# shellcheck shell=bash
# Fakes for robot/netguard/tests/{hosts,apply}.sh: ssh runs the robot half locally (SSH_CLIENT=$FAKE_SRC) against ETC_ROOT; iptables keeps
# the hooked chain's tcp 22 -s; LAN ssh obeys it, LAN :8888 is 000 while hooked, tailnet ssh/:8888 pass; escapepod.local by /etc/hosts.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/netguard-test.XXXXXX")
trap 'pkill -f -- "$tmp/" || :; rm -rf "$tmp"' EXIT # arm timers carry $tmp in argv
pc=192.0.2.249 pc_ts=100.64.0.2 bot_ts=100.64.0.9 bot_lan=192.0.2.76
export FAKE_PC=$pc FAKE_PC_TS=$pc_ts FAKE_BOT_LAN=$bot_lan
mkdir -p "$tmp/bin"
cat >"$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
while (($#)); do case $1 in -i | -o | -O) shift 2 ;; root@*) to=${1#root@}; shift; break ;; *) shift ;; esac; done
cmd=$*
ph=$(sed -n 's/^PHASE=\([a-z]*\) .*/\1/p' <<<"$cmd"); echo "ssh ${ph:-$cmd}" >>"$ETC_ROOT/calls"
n=$(grep -c "^ssh $ph$" "$ETC_ROOT/calls"); [[ -n $ph && $ph:$n == "${FAKE_SSH_FAIL:-}" ]] && exit 255 # FAKE_SSH_FAIL=<phase>:<nth call>
if [[ $to == "$FAKE_BOT_LAN" ]]; then # FAKE_LAN_SSH=open: the firewall does not bite
  [[ ${FAKE_LAN_SSH:-} != open && -e $ETC_ROOT/iptables.hooked ]] && ! grep -qx "$FAKE_PC" "$ETC_ROOT/iptables.live" && exit 255
elif [[ $cmd == true && ${FAKE_TS_SSH:-} == down ]]; then exit 255 # a fresh tailnet ssh fails; the mux phases still run
fi
[[ -n $cmd ]] || exit 0
SSH_CLIENT="${FAKE_SRC:-$FAKE_PC_TS} 50000 22" exec sh -c "$cmd"
EOF
cat >"$tmp/bin/mount" <<'EOF'
#!/bin/sh
case $* in *remount,rw*) m=rw ;; *) m=ro ;; esac
echo "/dev/root / ext4 $m 0 0" >"$ETC_ROOT/proc/mounts"
EOF
cat >"$tmp/bin/setsid" <<'EOF'
#!/bin/sh
echo setsid >>"$ETC_ROOT/calls"
exec "$@"
EOF
cat >"$tmp/bin/systemctl" <<'EOF'
#!/bin/sh
case $1 in
is-enabled) [ -e "$ETC_ROOT/enabled" ] ;;
enable) echo "systemctl enable" >>"$ETC_ROOT/calls"; : >"$ETC_ROOT/enabled" ;;
disable) rm -f "$ETC_ROOT/enabled" ;;
esac
EOF
cat >"$tmp/bin/iptables" <<'EOF'
#!/bin/sh
s=$ETC_ROOT/$(basename "$0")
case " $* " in
*" -N MYVECTOR_IN_NEW "* | *" -F MYVECTOR_IN_NEW "*) : >"$s.new" ;;
*" -A MYVECTOR_IN_NEW -s "*) echo "$4" >>"$s.new" ;;
*" -I INPUT -i wlan0 -j MYVECTOR_IN_NEW "*) : >"$s.newhooked" ;;
*" -D INPUT -i wlan0 -j MYVECTOR_IN_NEW "*) [ -e "$s.newhooked" ] && rm "$s.newhooked" ;;
*" -E MYVECTOR_IN_NEW MYVECTOR_IN "*) [ -z "$FAKE_E_FAIL" ] || exit 1; mv "$s.new" "$s.live"; rm -f "$s.newhooked"; : >"$s.hooked" ;;
*" -C INPUT -i wlan0 -j MYVECTOR_IN "*) [ -e "$s.hooked" ] ;;
*" -D INPUT -i wlan0 -j MYVECTOR_IN "*) [ -e "$s.hooked" ] && rm "$s.hooked" ;;
*" -D "*) exit 1 ;;
esac
EOF
cp "$tmp/bin/iptables" "$tmp/bin/ip6tables"
cat >"$tmp/bin/curl" <<'EOF'
#!/bin/sh
for u; do :; done
case $u in
*escapepod.local*) [ "$(awk '/escapepod\.local/ { print $1; exit }' "$ETC_ROOT/etc/hosts")" = "$FAKE_PC_TS" ] && printf 200 || printf 000 ;;
*"//$FAKE_BOT_LAN:8888"*) # FAKE_CURL=up: LAN :8888 open whatever is hooked
  if [ "${FAKE_CURL:-}" != up ] && [ -e "$ETC_ROOT/iptables.hooked" ]; then printf 000; else printf 200; fi ;;
*:8888*) [ "${FAKE_CURL:-}" = down ] && printf 000 || printf 200 ;;
esac
EOF
chmod +x "$tmp/bin/"*
# shellcheck source=robot/lib/tests/fakeclock.sh
. "$robot/lib/tests/fakeclock.sh"; fakeclock_install "$tmp/bin" # arm timers park: a slow run never meets a fired rollback
: >"$tmp/key"
# env_file [VAR=value…]: a robot.env (the D7 facts; later lines override); prints its path
env_file() { local f; f=$(mktemp "$tmp/robot.XXXXXX.env")
  printf '%s\n' ROBOT_ESN=0 "ROBOT_IP=$bot_ts" ROBOT_SERVER=x "ROBOT_KEY=$tmp/key" "ROBOT_TTS_URL=http://$pc_ts:8091" \
    "PC_LAN_IP=$pc" "ROBOT_LAN_IP=$bot_lan" "$@" >"$f"; echo "$f"; }
# fresh <name>: a fake robot root, / ro; prints its path
fresh() { local r=$tmp/$1; mkdir -p "$r/proc" "$r/etc"; echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"; echo "$r"; }
# run <root> [VAR=value…] -- [apply.sh args]: apply.sh with the fakes first on PATH; NETGUARD_OK under the root
run() {
  local r=$1; shift; local envs=(); while (($#)) && [[ $1 != -- ]]; do envs+=("$1"); shift; done; (($#)) && shift
  env ETC_ROOT="$r" ETC_ARM_SCALE=3 ROLLBACK_MIN=1 PATH="$tmp/bin:$PATH" ROBOT_ENV="$(env_file)" NETGUARD_OK="$r/ok" "${envs[@]}" \
    "$robot/netguard/apply.sh" "$@" 2>&1
}
calls() { cat "$1/calls" 2>/dev/null || :; }

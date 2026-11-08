# shellcheck shell=bash
# Fakes for robot/netguard/tests/{hosts,apply}.sh: ssh runs the robot half locally (SSH_CLIENT=$FAKE_SRC) against ETC_ROOT;
# iptables keeps the -s allowlist of the hooked chain; curl answers escapepod.local by /etc/hosts' first match and :8888 by that allowlist.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/netguard-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
pc=192.0.2.249
mkdir -p "$tmp/bin"
cat >"$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
while (($#)); do case $1 in -i | -o | -O) shift 2 ;; root@*) shift; break ;; *) shift ;; esac; done
cmd=$*
ph=$(sed -n 's/^PHASE=\([a-z]*\) .*/\1/p' <<<"$cmd"); echo "ssh ${ph:-$cmd}" >>"$ETC_ROOT/calls"
[[ -n $cmd ]] || exit 0
SSH_CLIENT="${FAKE_SRC:-192.0.2.249} 50000 22" exec sh -c "$cmd"
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
*" -E MYVECTOR_IN_NEW MYVECTOR_IN "*) mv "$s.new" "$s.live"; : >"$s.hooked" ;;
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
*escapepod.local*) [ "$(awk '/escapepod\.local/ { print $1; exit }' "$ETC_ROOT/etc/hosts")" = 192.0.2.249 ] && printf 200 || printf 000 ;;
*:8888*)
  case ${FAKE_CURL:-} in up) printf 200; exit ;; down) printf 000; exit ;; esac
  if [ -e "$ETC_ROOT/iptables.hooked" ] && ! grep -qx "${FAKE_SRC:-192.0.2.249}" "$ETC_ROOT/iptables.live"; then printf 000; else printf 200; fi ;;
esac
EOF
chmod +x "$tmp/bin/"*
: >"$tmp/key"
# env_file [tts_url]: a robot.env; prints its path
env_file() { local f; f=$(mktemp "$tmp/robot.XXXXXX.env")
  printf '%s\n' ROBOT_ESN=0 ROBOT_IP=192.0.2.9 ROBOT_SERVER=x "ROBOT_KEY=$tmp/key" "ROBOT_TTS_URL=${1-http://$pc:8091}" >"$f"; echo "$f"; }
# fresh <name>: a fake robot root, / ro; prints its path
fresh() { local r=$tmp/$1; mkdir -p "$r/proc" "$r/etc"; echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"; echo "$r"; }
# run <root> [VAR=value…] -- [apply.sh args]: apply.sh with the fakes first on PATH; NETGUARD_OK under the root
run() {
  local r=$1; shift; local envs=(); while (($#)) && [[ $1 != -- ]]; do envs+=("$1"); shift; done; (($#)) && shift
  env ETC_ROOT="$r" ETC_ARM_SCALE=3 ROLLBACK_MIN=1 PATH="$tmp/bin:$PATH" ROBOT_ENV="$(env_file)" NETGUARD_OK="$r/ok" "${envs[@]}" \
    "$robot/netguard/apply.sh" "$@" 2>&1
}
calls() { cat "$1/calls" 2>/dev/null || :; }

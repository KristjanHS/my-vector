#!/usr/bin/env bash
# Pin escapepod.local to the ROBOT_TTS_URL host (laptop tailnet IP) in the robot's /etc/hosts, then the wlan0 firewall (robot-tailscale
# D8/D10) when NETGUARD_OK holds d:<PC_LAN_IP>. Usage: robot/netguard/apply.sh [--selftest]   idempotent; settings.sh runs it.
# --selftest: decoy phases must bite, then writes NETGUARD_OK. Exit 0 = applied (or WARN printed), 1 = FAIL, 2 = SETUP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"
selftest=0
for a; do case $a in --selftest) selftest=1 ;; *) echo "SETUP: netguard: unknown arg $a (--selftest)" >&2; exit 2 ;; esac; done
pin=${tts_url#*://} pin=${pin%%[:/]*} pc_lan=${PC_LAN_IP:-} robot_lan=${ROBOT_LAN_IP:-}
for kv in "ROBOT_TTS_URL host=$pin" "PC_LAN_IP=$pc_lan" "ROBOT_IP=$ip" "ROBOT_LAN_IP=$robot_lan"; do
  [[ ${kv#*=} =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || { echo "SETUP: netguard: $kv is not an IPv4 address (D7)" >&2; exit 2; }
done
[[ $ip != "$robot_lan" ]] || { echo "SETUP: netguard: ROBOT_IP = ROBOT_LAN_IP ($ip): ROBOT_IP must be the robot's tailnet IP (D7)" >&2; exit 2; }
in_place=0 decoy=192.0.2.1 ts_port=41641 ok_file=${NETGUARD_OK:-${robot_dir%/robot}/secrets/netguard-selftest.ok} force=
ok_tag=d:$pc_lan # d: = the D8 shape; a pre-D marker forces a fresh --selftest
((selftest)) && force=FORCE=1
die() { echo "FAIL: netguard: $*" >&2; exit 1; }

# remote <phase> [VAR=value…]: the robot half below, after lib/etc.sh
remote() {
  local phase=$1; shift
  # shellcheck disable=SC2029 # the VARs expand on the client: the point
  cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "PHASE=$phase SSH_OK=$pin,$pc_lan $* sh -s"
set -e
hosts=/etc/hosts bin=/etc/myvector/netguard.sh unit=/etc/systemd/system/myvector-netguard.service run=/run/myvector/netguard.sh
restore='for t in iptables ip6tables; do for c in MYVECTOR_IN MYVECTOR_IN_NEW; do while $t -D INPUT -i wlan0 -j $c 2>/dev/null; do :; done; done; done
if systemctl is-enabled -q myvector-netguard 2>/dev/null; then mount -o remount,rw / && systemctl disable myvector-netguard 2>/dev/null; mount -o remount,ro /; fi'
hooked() { iptables -C INPUT -i wlan0 -j MYVECTOR_IN 2>/dev/null && ip6tables -C INPUT -i wlan0 -j MYVECTOR_IN 2>/dev/null; }
case $PHASE in
hosts) # PIN=<ip> [REACH=1]
  [ "$(readlink -f "$ETC_ROOT$hosts")" = "$(readlink -f "$ETC_ROOT/etc")/hosts" ] || { echo "FAIL: $hosts is a link off /etc"; exit 1; }
  line="$PIN escapepod.local # my-vector"
  etc_put $hosts "$({ echo "$line"; grep -v ' # my-vector$' "$ETC_ROOT$hosts" 2>/dev/null || :; })"
  etc_ro
  if [ -n "$ETC_CHANGED" ]; then echo "netguard: pinned escapepod.local to $PIN"; else echo "netguard: escapepod.local pinned to $PIN already"; fi
  [ -z "${REACH:-}" ] || echo "reach=$(curl -sk -m 4 -o /dev/null -w '%{http_code}' https://escapepod.local/ok || :)" ;;
fw) # SCRIPT=<base64 rules.sh output> [FORCE=1]
  case ",$SSH_OK," in *",${SSH_CLIENT%% *},"*) ;; *) echo "FAIL: ssh comes from ${SSH_CLIENT%% *}, not one of $SSH_OK: refusing (lock-out guard)"; exit 1 ;; esac
  new=$(echo "$SCRIPT" | base64 -d)
  if [ -z "${FORCE:-}" ] && [ "$(cat "$ETC_ROOT$bin" 2>/dev/null)" = "$new" ] && [ -f "$ETC_ROOT$unit" ] && hooked &&
    [ -e "$ETC_ROOT/run/myvector/netguard.confirm" ]; then # confirmed since boot, no arm pending: the live chain is $bin
    echo "netguard: firewall in place"; exit 0
  fi
  arm netguard "$ROLLBACK_MIN" "$restore"
  mkdir -p "$ETC_ROOT${run%/*}"
  printf '%s\n' "$new" >"$ETC_ROOT$run"
  sh "$ETC_ROOT$run"
  echo "netguard: firewall applied live (rollback armed, $ROLLBACK_MIN min)" ;;
undo)
  eval "$restore"; echo "netguard: firewall unhooked" ;;
confirm) # SCRIPT
  confirm netguard
  etc_put $bin "$(echo "$SCRIPT" | base64 -d)"
  etc_put $unit "[Unit]
Description=my-vector netguard: wlan0 input firewall (robot/netguard)
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh $bin
ExecStop=/bin/sh $bin stop

[Install]
WantedBy=multi-user.target"
  if [ -n "$ETC_CHANGED" ] || ! systemctl is-enabled -q myvector-netguard 2>/dev/null; then
    etc_rw; systemctl daemon-reload; systemctl enable myvector-netguard 2>/dev/null; etc_ro
    echo "netguard: installed $bin + myvector-netguard.service"
  fi
  etc_ro_check ;;
esac
EOF
}
# reach <ip>: http code of the robot's :8888 from here (a new connection: the firewall decides)
reach() { curl -s -m 3 -o /dev/null -w '%{http_code}' "http://$1:8888/consolevars" || :; }
ssh_new() { ssh -i "$key" "${ssh_base[@]}" -o ControlPath=none "root@$1" true 2>/dev/null && echo ok || echo refused; }
# fw <ssh-from> <want LAN ssh: ok|refused>: a fresh tailnet ssh before arm; after the live apply ROBOT_IP ssh ok + :8888 200,
# ROBOT_LAN_IP ssh <want> + :8888 000 (D10); undo at once on a miss
fw() {
  local from=$1 want=$2 script out
  [[ $(ssh_new "$ip") == ok ]] || die "fresh ssh to ROBOT_IP $ip refused: not arming (lock-out guard)"
  script=$(bash "$here/rules.sh" "$from" "$ts_port" | base64 -w0)
  out=$(remote fw "SCRIPT=$script" $force) || { remote undo >/dev/null 2>&1 || :; die "${out:-fw phase failed}; unhooked"; }
  echo "$out"
  [[ $out == *"in place"* ]] && { in_place=1; return 0; }
  local got
  got="$(ssh_new "$ip") $(reach "$ip") $(ssh_new "$robot_lan") $(reach "$robot_lan")"
  if [[ $got != "ok 200 $want 000" ]]; then
    remote undo >/dev/null 2>&1 || :
    die "ssh from $from: ROBOT_IP ssh + :8888, ROBOT_LAN_IP ssh + :8888 = [$got] (want [ok 200 $want 000]); unhooked"
  fi
}

if ((selftest)); then
  trap 'remote hosts "PIN=$pin" >/dev/null 2>&1 || :' EXIT # a failure below must not leave the decoy pinned
  out=$(remote hosts "PIN=$decoy REACH=1") || die "$out"
  [[ $out == *reach=200* ]] && die "decoy pin $decoy: escapepod.local/ok still 200 (the pin did not bite)"
  out=$(remote hosts "PIN=$pin REACH=1") || die "$out"
  trap - EXIT
  [[ $out == *reach=200* ]] || die "pin $pin: escapepod.local/ok = ${out##*reach=} (want 200)"
  echo "${out%%$'\n'reach=*} (selftest: decoy bit, $pin 200)"
else
  out=$(remote hosts "PIN=$pin") || die "$out"
  echo "$out"
fi

if ((!selftest)) && [[ $(cat "$ok_file" 2>/dev/null) != "$ok_tag" ]]; then
  echo "WARN: netguard firewall: not applied, no selftest for $ok_tag — run robot/netguard/apply.sh --selftest"
  exit 0
fi
if ((selftest)); then fw "$decoy" refused; fi
fw "$pc_lan" ok
((in_place)) || remote confirm "SCRIPT=$(bash "$here/rules.sh" "$pc_lan" "$ts_port" | base64 -w0)" || die "confirm phase failed"
if ((selftest)); then printf '%s\n' "$ok_tag" >"$ok_file"; echo "PASS: netguard selftest ($ok_tag): wrote $ok_file"; fi

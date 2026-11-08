#!/usr/bin/env bash
# Pin escapepod.local to pc_ip in the robot's /etc/hosts (N4), then the wlan0 firewall (N2/N3) when NETGUARD_OK holds pc_ip (N5).
# Usage: robot/netguard/apply.sh [--selftest]   idempotent; settings.sh runs it. --selftest: decoy phases must bite, then writes NETGUARD_OK.
# pc_ip = the IPv4 host of ROBOT_TTS_URL (N1). Exit 0 = applied (or WARN printed), 1 = FAIL, 2 = SETUP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
selftest=0
for a; do case $a in --selftest) selftest=1 ;; *) echo "SETUP: netguard: unknown arg $a (--selftest)" >&2; exit 2 ;; esac; done
pc_ip=${tts_url#*://} pc_ip=${pc_ip%%[:/]*}
[[ $pc_ip =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
  { echo "SETUP: netguard: ROBOT_TTS_URL host '$pc_ip' is not an IPv4 address (pc_ip, plan N1)" >&2; exit 2; }
decoy=192.0.2.1 ok_file=${NETGUARD_OK:-${robot_dir%/robot}/secrets/netguard-selftest.ok} force=
((selftest)) && force=FORCE=1
die() { echo "FAIL: netguard: $*" >&2; exit 1; }

# remote <phase> [VAR=value…]: the robot half below, after lib/etc.sh
remote() {
  local phase=$1; shift
  # shellcheck disable=SC2029 # the VARs expand on the client: the point
  cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "PHASE=$phase PC_IP=$pc_ip $* sh -s"
set -e
hosts=/etc/hosts bin=/etc/myvector/netguard.sh unit=/etc/systemd/system/myvector-netguard.service run=/run/myvector/netguard.sh
restore='for t in iptables ip6tables; do while $t -D INPUT -i wlan0 -j MYVECTOR_IN 2>/dev/null; do :; done; done
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
  [ "${SSH_CLIENT%% *}" = "$PC_IP" ] || { echo "FAIL: ssh comes from ${SSH_CLIENT%% *}, not pc_ip $PC_IP: refusing (lock-out guard)"; exit 1; }
  new=$(echo "$SCRIPT" | base64 -d)
  if [ -z "${FORCE:-}" ] && [ "$(cat "$ETC_ROOT$bin" 2>/dev/null)" = "$new" ] && [ -f "$ETC_ROOT$unit" ] && hooked; then
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
ssh_new() { ssh -i "$key" "${ssh_base[@]}" -o ControlPath=none "root@$ip" true; }
# fw <allow4…> <want 8888 code: 200|000>: live apply, then a fresh ssh + :8888 from here; undo at once on a miss
fw() {
  local want=${*: -1} script out
  script=$(bash "$here/rules.sh" "${@:1:$#-1}" | base64 -w0)
  out=$(remote fw "SCRIPT=$script" $force) || die "${out:-fw phase failed}"
  echo "$out"
  [[ $out == *"in place"* ]] && return 0
  local code; code=$(reach "$ip")
  if ! ssh_new 2>/dev/null || [[ $code != "$want" ]]; then
    remote undo >/dev/null 2>&1 || :
    die "allow [${*:1:$#-1}]: new ssh $(ssh_new 2>/dev/null && echo ok || echo refused), :8888 = $code (want $want); unhooked"
  fi
}

if ((selftest)); then
  out=$(remote hosts "PIN=$decoy REACH=1") || die "$out"
  [[ $out == *reach=200* ]] && { remote hosts "PIN=$pc_ip" >/dev/null || :; die "decoy pin $decoy: escapepod.local/ok still 200 (the pin did not bite)"; }
  out=$(remote hosts "PIN=$pc_ip REACH=1") || die "$out"
  [[ $out == *reach=200* ]] || die "pin $pc_ip: escapepod.local/ok = ${out##*reach=} (want 200)"
  echo "${out%%$'\n'reach=*} (selftest: decoy bit, pc_ip 200)"
else
  out=$(remote hosts "PIN=$pc_ip") || die "$out"
  echo "$out"
fi

if ((!selftest)) && [[ $(cat "$ok_file" 2>/dev/null) != "$pc_ip" ]]; then
  echo "WARN: netguard firewall: not applied, no selftest for pc_ip $pc_ip — run robot/netguard/apply.sh --selftest"
  exit 0
fi
if ((selftest)); then fw "$decoy" 000; fi
fw "$pc_ip" 200
remote confirm "SCRIPT=$(bash "$here/rules.sh" "$pc_ip" | base64 -w0)" || die "confirm phase failed"
if ((selftest)); then printf '%s\n' "$pc_ip" >"$ok_file"; echo "PASS: netguard selftest ($pc_ip): wrote $ok_file"; fi

#!/usr/bin/env bash
# Render the robot's wlan0 input firewall (robot-tailscale D8) to stdout: a POSIX-sh script, `sh <it>` applies, `sh <it> stop` unhooks.
# Usage: robot/netguard/rules.sh <ssh-from-ipv4> <ts-udp-port>   exit 2 on a bad arg. Never names OUTPUT/tailscale0 (D13: TS_UNIT's).
set -euo pipefail
(($# == 2)) && [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ && $2 =~ ^[1-9][0-9]{0,4}$ ]] ||
  { echo "SETUP: netguard rules: want <ssh-from-ipv4> <ts-udp-port>, got [$*]" >&2; exit 2; }
echo '#!/bin/sh'
echo "ssh_from=$1 ts_port=$2"
cat <<'EOF'
# my-vector netguard (rendered by robot/netguard/rules.sh): chain MYVECTOR_IN, jumped from INPUT -i wlan0 only.
set -e
unhook() { while "$1" -D INPUT -i wlan0 -j "$2" 2>/dev/null; do :; done; }
if [ "${1:-}" = stop ]; then
  for t in iptables ip6tables; do unhook $t MYVECTOR_IN; $t -F MYVECTOR_IN 2>/dev/null || :; $t -X MYVECTOR_IN 2>/dev/null || :; done
  exit 0
fi
# Build in MYVECTOR_IN_NEW (any failure exits before a hook changes), then swap it in for MYVECTOR_IN.
fresh() { unhook "$1" MYVECTOR_IN_NEW; "$1" -N MYVECTOR_IN_NEW 2>/dev/null || "$1" -F MYVECTOR_IN_NEW; }
est() { "$1" -A MYVECTOR_IN_NEW -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null ||
  "$1" -A MYVECTOR_IN_NEW -m state --state ESTABLISHED,RELATED -j ACCEPT; }
v4() { iptables -A MYVECTOR_IN_NEW "$@"; }
v6() { ip6tables -A MYVECTOR_IN_NEW "$@"; }
fresh iptables
est iptables
v4 -s "$ssh_from" -p tcp --dport 22 -j ACCEPT
v4 -p udp --dport "$ts_port" -j ACCEPT
v4 -p udp --sport 67 --dport 68 -j ACCEPT
v4 -p icmp --icmp-type echo-request -j ACCEPT
v4 -j DROP
fresh ip6tables
est ip6tables
v6 -p ipv6-icmp -j ACCEPT
v6 -p udp --dport 546 -j ACCEPT
v6 -p udp --dport "$ts_port" -j ACCEPT
v6 -j DROP
for t in iptables ip6tables; do
  $t -I INPUT -i wlan0 -j MYVECTOR_IN_NEW
  unhook $t MYVECTOR_IN
  $t -F MYVECTOR_IN 2>/dev/null || :
  $t -X MYVECTOR_IN 2>/dev/null || :
  $t -E MYVECTOR_IN_NEW MYVECTOR_IN
done
EOF

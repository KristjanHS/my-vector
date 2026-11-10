#!/usr/bin/env bash
# Host test of robot/netguard/apply.sh's firewall (robot-tailscale D10, make check): fakes in tests/fakes.sh.
set -euo pipefail
# shellcheck source=robot/netguard/tests/fakes.sh
. "$(dirname "$0")/fakes.sh"
die() { echo "FAIL: netguard apply test: $*" >&2; exit 1; }
unit=etc/systemd/system/myvector-netguard.service
seq_of() { grep -E '^(ssh (hosts|live|fw|undo|confirm)|setsid|systemctl enable)' "$1/calls" | paste -sd'|'; }

for stale in d:10.0.0.7 "$pc"; do # another PC_LAN_IP; a pre-D marker
  r=$(fresh "gate${stale//[:.]/}"); echo "$stale" >"$r/ok"
  out=$(run "$r") || die "stale NETGUARD_OK [$stale] run: $out"
  [[ $out == *"WARN: netguard firewall: not applied"* && $(seq_of "$r") == 'ssh hosts' ]] || die "NETGUARD_OK [$stale]: out=[$out] calls=[$(seq_of "$r")]"
done

r=$(fresh self)
out=$(run "$r" -- --selftest) || die "selftest: $out"
want='ssh hosts|ssh hosts|ssh fw|setsid|ssh fw|setsid|ssh confirm|systemctl enable'
[[ $(seq_of "$r") == "$want" ]] || die "selftest calls [$(seq_of "$r")], want [$want]"
[[ $(cat "$r/ok") == "d:$pc" && $out == *"PASS: netguard selftest"* ]] || die "selftest: no NETGUARD_OK / PASS: $out"
[[ -f $r/$unit && -e $r/run/myvector/netguard.confirm && -e $r/iptables.hooked && -e $r/ip6tables.hooked ]] || die "selftest: unit, confirm or hooks missing"
[[ $(cat "$r/iptables.live") == "$pc" ]] || die "live allowlist [$(cat "$r/iptables.live")], want $pc"
grep -q "ExecStart=/bin/sh /etc/myvector/netguard.sh" "$r/$unit" && cmp -s "$r/etc/myvector/netguard.sh" <(bash "$robot/netguard/rules.sh" "$pc" 41641) ||
  die "installed unit/script differ from rules.sh $pc"
out=$(run "$r") || die "2nd run: $out"
[[ $out == *"firewall in place"* ]] && ! grep -q setsid <(seq_of "$r" | sed "s/^$want//") || die "2nd run re-armed: out=[$out] calls=[$(seq_of "$r")]"

for c in down up; do # tailnet :8888 down; LAN :8888 still 200 after apply
  r=$(fresh "curl$c"); echo "d:$pc" >"$r/ok"
  if out=$(run "$r" FAKE_CURL=$c); then die "FAKE_CURL=$c in the real phase passed: $out"; fi
  [[ ! -e $r/run/myvector/netguard.confirm && ! -f $r/$unit && ! -e $r/iptables.hooked ]] || die "FAKE_CURL=$c: confirmed, installed the unit, or left the hook: calls=[$(seq_of "$r")]"
  [[ $(seq_of "$r") == 'ssh hosts|ssh fw|setsid|ssh undo' ]] || die "FAKE_CURL=$c calls [$(seq_of "$r")]"
done

r=$(fresh nobite)
if out=$(run "$r" FAKE_LAN_SSH=open -- --selftest); then die "selftest with LAN ssh still accepted under the decoy passed: $out"; fi
[[ $out == *"want [ok 200 refused 000]"* && ! -e $r/ok && ! -f $r/$unit && ! -e $r/iptables.hooked ]] || die "no-bite selftest: out=[$out] or wrote ok/unit/hook"

r=$(fresh away); echo "d:$pc" >"$r/ok"
if out=$(run "$r" FAKE_SRC=10.9.9.9); then die "ssh from neither PC_LAN_IP nor the laptop tailnet IP passed: $out"; fi
[[ $out == *"lock-out guard"* && ! -e $r/iptables.hooked ]] && ! grep -q setsid "$r/calls" || die "foreign source: out=[$out] calls=[$(seq_of "$r")]"
r=$(fresh lansrc); echo "d:$pc" >"$r/ok"
out=$(run "$r" FAKE_SRC="$pc") && [[ -e $r/run/myvector/netguard.confirm ]] || die "ssh from PC_LAN_IP refused: $out"
r=$(fresh tsdown); echo "d:$pc" >"$r/ok"
if out=$(run "$r" FAKE_TS_SSH=down); then die "fresh tailnet ssh down passed: $out"; fi
[[ $out == *"not arming"* ]] && ! grep -qE 'setsid|ssh fw' "$r/calls" || die "tailnet ssh down: out=[$out] calls=[$(seq_of "$r")]"
# a pending arm (no confirm file) with the decoy live is not "in place": re-applied and confirmed
r=$(fresh pending); out=$(run "$r" -- --selftest) || die "pending: selftest: $out"
rm "$r/run/myvector/netguard.confirm"; echo 192.0.2.1 >"$r/iptables.live"
out=$(run "$r") || die "pending: run: $out"
[[ $out != *"in place"* && $(cat "$r/iptables.live") == "$pc" && -e $r/run/myvector/netguard.confirm ]] || die "pending arm taken as in place: out=[$out]"

# rebooted (/run/myvector emptied), PC off the robot LAN: WARN, exit 0, the boot unit's firewall untouched (no arm/undo/disable)
r=$(fresh offlan); out=$(run "$r" -- --selftest) || die "offlan: selftest: $out"
rm "$r/run/myvector/netguard.confirm" "$r/run/myvector/netguard.sh"; : >"$r/calls"
out=$(run "$r" FAKE_LAN_SSH=down) || die "offlan: run exited non-zero: $out"
[[ $out == *"WARN: PC not on the robot LAN ($bot_lan ssh refused)"* && $(seq_of "$r") == 'ssh hosts|ssh live' ]] || die "offlan: out=[$out] calls=[$(seq_of "$r")]"
[[ -e $r/enabled && -e $r/iptables.hooked && -e $r/ip6tables.hooked ]] || die "offlan: unit disabled or hook removed: calls=[$(seq_of "$r")]"
r=$(fresh offlanself)
if out=$(run "$r" FAKE_LAN_SSH=down -- --selftest); then die "offlan selftest passed: $out"; fi
[[ $out == *"selftest needs LAN ssh"* && ! -e $r/ok && $(seq_of "$r") == 'ssh hosts|ssh hosts|ssh live' ]] || die "offlan selftest: out=[$out] calls=[$(seq_of "$r")]"

r=$(fresh swapfail); echo "d:$pc" >"$r/ok"
if out=$(run "$r" FAKE_E_FAIL=1); then die "a failed swap passed: $out"; fi
[[ ! -e $r/iptables.newhooked && $(seq_of "$r") == "ssh hosts|ssh fw|setsid|ssh undo" ]] || die "failed swap: _NEW left hooked or no undo: calls=[$(seq_of "$r")]"

r=$(fresh repin)
if out=$(run "$r" FAKE_SSH_FAIL=hosts:2 -- --selftest); then die "selftest with a failed tailnet re-pin passed: $out"; fi
[[ $(awk '/escapepod/ { print $1; exit }' "$r/etc/hosts") == "$pc_ts" ]] || die "selftest died with the decoy pinned: [$(cat "$r/etc/hosts")]"
echo "netguard apply host test PASS"

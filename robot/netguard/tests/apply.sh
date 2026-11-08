#!/usr/bin/env bash
# Host test of robot/netguard/apply.sh's firewall (N3/N5, make check): fakes in tests/fakes.sh; the fake curl's :8888 obeys the hooked allowlist.
set -euo pipefail
# shellcheck source=robot/netguard/tests/fakes.sh
. "$(dirname "$0")/fakes.sh"
die() { echo "FAIL: netguard apply test: $*" >&2; exit 1; }
unit=etc/systemd/system/myvector-netguard.service
seq_of() { grep -E '^(ssh (hosts|fw|undo|confirm)|setsid|systemctl enable)' "$1/calls" | paste -sd'|'; }

r=$(fresh gate); echo 10.0.0.7 >"$r/ok"
out=$(run "$r") || die "stale NETGUARD_OK run: $out"
[[ $out == *"WARN: netguard firewall: not applied"* && $(seq_of "$r") == 'ssh hosts' ]] || die "NETGUARD_OK for another ip: out=[$out] calls=[$(seq_of "$r")]"

r=$(fresh self)
out=$(run "$r" -- --selftest) || die "selftest: $out"
want='ssh hosts|ssh hosts|ssh fw|setsid|ssh fw|setsid|ssh confirm|systemctl enable'
[[ $(seq_of "$r") == "$want" ]] || die "selftest calls [$(seq_of "$r")], want [$want]"
[[ $(cat "$r/ok") == "$pc" && $out == *"PASS: netguard selftest"* ]] || die "selftest: no NETGUARD_OK / PASS: $out"
[[ -f $r/$unit && -e $r/run/myvector/netguard.confirm && -e $r/iptables.hooked && -e $r/ip6tables.hooked ]] || die "selftest: unit, confirm or hooks missing"
[[ $(cat "$r/iptables.live") == "$pc" ]] || die "live allowlist [$(cat "$r/iptables.live")], want $pc"
grep -q "ExecStart=/bin/sh /etc/myvector/netguard.sh" "$r/$unit" && cmp -s "$r/etc/myvector/netguard.sh" <(bash "$robot/netguard/rules.sh" "$pc") ||
  die "installed unit/script differ from rules.sh $pc"
out=$(run "$r") || die "2nd run: $out"
[[ $out == *"firewall in place"* ]] && ! grep -q setsid <(seq_of "$r" | sed "s/^$want//") || die "2nd run re-armed: out=[$out] calls=[$(seq_of "$r")]"

r=$(fresh down); echo "$pc" >"$r/ok"
if out=$(run "$r" FAKE_CURL=down); then die ":8888 down in the real phase passed: $out"; fi
[[ ! -e $r/run/myvector/netguard.confirm && ! -f $r/$unit && ! -e $r/iptables.hooked ]] || die ":8888 down: confirmed, installed the unit, or left the hook: calls=[$(seq_of "$r")]"
[[ $(seq_of "$r") == 'ssh hosts|ssh fw|setsid|ssh undo' ]] || die ":8888 down calls [$(seq_of "$r")]"

r=$(fresh nobite)
if out=$(run "$r" FAKE_CURL=up -- --selftest); then die "selftest with a decoy that did not bite passed: $out"; fi
[[ $out == *"want 000"* && ! -e $r/ok && ! -f $r/$unit && ! -e $r/iptables.hooked ]] || die "no-bite selftest: out=[$out] or wrote ok/unit/hook"

r=$(fresh away); echo "$pc" >"$r/ok"
if out=$(run "$r" FAKE_SRC=10.9.9.9); then die "ssh from a non-pc_ip source passed: $out"; fi
[[ $out == *"lock-out guard"* && ! -e $r/iptables.hooked ]] && ! grep -q setsid "$r/calls" || die "foreign source: out=[$out] calls=[$(seq_of "$r")]"
echo "netguard apply host test PASS"

#!/usr/bin/env bash
# Host test of robot/netguard/rules.sh (make check): runs the rendered script under sh -e with fake iptables/ip6tables that log argv.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/netguard-rules-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: netguard rules test: $*" >&2; exit 1; }
mkdir "$tmp/bin"
# -D/-C fail (no such rule), -N/-F/-X/-E always succeed; FAKE_NO_CONNTRACK=1 refuses -m conntrack
cat >"$tmp/bin/iptables" <<'EOF'
#!/bin/sh
echo "$(basename "$0") $*" >>"$LOG"
case " $* " in *" -D "* | *" -C "*) exit 1 ;; *" -m conntrack "*) [ -z "$FAKE_NO_CONNTRACK" ] || exit 2 ;; esac
EOF
chmod +x "$tmp/bin/iptables"; cp "$tmp/bin/iptables" "$tmp/bin/ip6tables"
run() { bash "$robot/netguard/rules.sh" "$@" >"$tmp/ng.sh" && : >"$tmp/log" && LOG=$tmp/log PATH="$tmp/bin:$PATH" sh -e "$tmp/ng.sh"; }
v4() { grep '^iptables -A MYVECTOR_IN_NEW' "$tmp/log" | sed 's/^iptables -A MYVECTOR_IN_NEW //'; }

run 192.0.2.249 10.0.0.7 || die "render/run failed"
want='-m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
-p tcp --dport 22 -j ACCEPT
-s 192.0.2.249 -j ACCEPT
-s 10.0.0.7 -j ACCEPT
-p udp --sport 67 --dport 68 -j ACCEPT
-p icmp --icmp-type echo-request -j ACCEPT
-j DROP'
[[ $(v4) == "$want" ]] || die "v4 rules (N2 order, DROP last): got
$(v4)"
want6='-m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
-p ipv6-icmp -j ACCEPT
-p tcp --dport 22 -j ACCEPT
-p udp --dport 546 -j ACCEPT
-j DROP'
got6=$(grep '^ip6tables -A MYVECTOR_IN_NEW' "$tmp/log" | sed 's/^ip6tables -A MYVECTOR_IN_NEW //')
[[ $got6 == "$want6" ]] || die "v6 rules: got
$got6"
# every allow rule (both families) lands before the first hook change
first_hook=$(grep -n ' -I INPUT ' "$tmp/log" | head -1 | cut -d: -f1)
last_add=$(grep -n ' -A MYVECTOR_IN_NEW ' "$tmp/log" | tail -1 | cut -d: -f1)
[[ -n $first_hook ]] && ((last_add < first_hook)) || die "a rule is added after the hook swap began (add line $last_add, hook line $first_hook)"
for t in iptables ip6tables; do
  grep -qx "$t -I INPUT -i wlan0 -j MYVECTOR_IN_NEW" "$tmp/log" && grep -qx "$t -E MYVECTOR_IN_NEW MYVECTOR_IN" "$tmp/log" ||
    die "$t: no wlan0 hook of MYVECTOR_IN_NEW, or no rename to MYVECTOR_IN"
done
grep -E ' -P | -A INPUT | -I INPUT| -D INPUT' "$tmp/log" | grep -v -- ' -i wlan0 -j MYVECTOR_IN' && die "touched the INPUT policy or a non-wlan0 hook"
grep -E -- ' -[io] ' "$tmp/log" | grep -v -- '-i wlan0' && die "matched an interface other than wlan0"

FAKE_NO_CONNTRACK=1 run 192.0.2.249 || die "no-conntrack run failed"
grep -qx 'iptables -A MYVECTOR_IN_NEW -m state --state ESTABLISHED,RELATED -j ACCEPT' "$tmp/log" || die "no -m state fallback without conntrack"

: >"$tmp/log"; LOG=$tmp/log PATH="$tmp/bin:$PATH" sh -e "$tmp/ng.sh" stop || die "stop failed"
grep -qx 'iptables -D INPUT -i wlan0 -j MYVECTOR_IN' "$tmp/log" && ! grep -q ' -A ' "$tmp/log" || die "stop: no unhook, or it added rules"

for bad in '' 192.168.1 2001:db8::1 'http://192.0.2.249'; do
  rc=0; bash "$robot/netguard/rules.sh" ${bad:+"$bad"} >/dev/null 2>&1 || rc=$?
  ((rc == 2)) || die "allowlist [$bad]: exit $rc, want 2"
done
echo "netguard rules host test PASS"

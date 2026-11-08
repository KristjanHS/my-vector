#!/usr/bin/env bash
# Host test of robot/netguard/apply.sh's hosts pin (N4, make check): fakes in tests/fakes.sh; no NETGUARD_OK, so no firewall.
set -euo pipefail
# shellcheck source=robot/netguard/tests/fakes.sh
. "$(dirname "$0")/fakes.sh"
die() { echo "FAIL: netguard hosts test: $*" >&2; exit 1; }

r=$(fresh pin)
printf '%s\n' '127.0.0.1 localhost' '192.168.1.5 escapepod.local # my-vector' '10.1.1.1 foo # mine' '::1 localhost ip6-localhost' >"$r/etc/hosts"
out=$(run "$r") || die "1st run: $out"
want="$pc escapepod.local # my-vector
127.0.0.1 localhost
10.1.1.1 foo # mine
::1 localhost ip6-localhost"
[[ $(cat "$r/etc/hosts") == "$want" ]] || die "hosts = [$(cat "$r/etc/hosts")], want one marked pc_ip line first, foreign lines unchanged"
[[ $out == *"pinned escapepod.local to $pc"* && $out == *"WARN: netguard firewall: not applied"* ]] || die "1st run out: $out"
grep -q '^ro' <(awk '$2 == "/" { print $4 }' "$r/proc/mounts") || die "/ left rw"
sum=$(stat -c %Y.%i "$r/etc/hosts")
sleep 1
out=$(run "$r") || die "2nd run: $out"
[[ $out == *"pinned to $pc already"* && $(stat -c %Y.%i "$r/etc/hosts") == "$sum" ]] || die "2nd run wrote /etc/hosts: $out"
grep -q 'ssh fw' <(calls "$r") && die "no NETGUARD_OK, but a fw phase ran"

r=$(fresh link); mkdir -p "$r/data"; echo '127.0.0.1 localhost' >"$r/data/hosts"; ln -s ../data/hosts "$r/etc/hosts"
if out=$(run "$r"); then die "/etc/hosts link off /etc passed: $out"; fi
[[ -L $r/etc/hosts && $(cat "$r/data/hosts") == '127.0.0.1 localhost' ]] || die "linked /etc/hosts was rewritten"

for url in http://escapepod.local:8091 '' 'http://[2001:db8::1]:8091'; do
  rc=0; out=$(run "$(fresh bad)" ROBOT_ENV="$(env_file "$url")") || rc=$?
  ((rc == 2)) || die "ROBOT_TTS_URL [$url]: exit $rc, want 2 (SETUP): $out"
done
echo "netguard hosts host test PASS"

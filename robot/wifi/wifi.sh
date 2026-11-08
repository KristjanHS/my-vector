#!/usr/bin/env bash
# shellcheck disable=SC2016 # the single-quoted snippets expand on the robot
# The robot's saved WiFi networks (connman, /data/lib/connman/<service>/settings: root-only 0700/0600, survives make ship).
# At boot connman rejoins the network it used last (2026-11-07: booted on floor 1 next to the stronger saved Linwifi2_1k,
# joined Linwifi2_2k ~1 s after wlan0 came up — the only one with a saved Frequency=), and it never leaves a working
# network for a stronger one: switch moves it. The firmware forgets a network only on a BLE "forget".
# Usage: wifi.sh list               lines "saved|seen|connected <ssid-hex>" (seen = in range now)
#        wifi.sh add                stdin: line 1 SSID, line 2 password (WPA/WPA2-PSK) — never argv, never a file on this PC
#        wifi.sh forget <ssid-hex>
#        wifi.sh test|switch <ssid-hex>   start a job on the robot and return: join that saved network, check the robot
#                                   kept its address and reaches wire-pod (https://escapepod.local/ok), then test goes
#                                   back and switch stays; any failed check goes back. SSH drops while it runs.
#                                   Firmware 0013 keeps vic-switchboard's own reconnect out while $j says "running:"
#                                   (path and prefix are shared with wifiWatcher.cpp; each step rewrites $j).
#        wifi.sh status             the job's line: none | running: … | ok: … | failed: …
#        wifi.sh roam               fresh iw scan; when a saved network is >= 8 dB above the current one, switch to it
#                                   ("ok: roam …"), else print nothing. A failed switch target is skipped for 60 min.
#                                   iw dBm, not connman's Strength: that read 30 for the connected network next to a stale
#                                   73 for an idle one (2026-11-07; iw: -61 vs -47 dBm).
#                                   wire-pod runs it every few minutes (myvector/wifi Roam). SSIDs with a backslash or
#                                   edge spaces never match iw's escaped names, so they are never roamed to.
#   add/forget only change connman's files and never restart connman: the robot stays online, and the change
#   counts from its next boot (a restart can get a new DHCP address, so the robot looks offline: 2026-11-07).
#   Re-saving a saved network drops connman's in-memory copy (else it saves the old password back): until the next
#   boot connman will not fail over to that one. list counts a network as saved only while its file holds a password.
#   The connected network is refused (a wrong password would strand the robot until BLE setup). wire-pod's WiFi page runs this.
#   Exit 0 = done; 1 = bad input or SSH failed; 2 = SETUP (robot/env.sh); 3 = refused: the robot is on that network now;
#   4 = refused: a test/switch job is running.
set -uo pipefail
export LC_ALL=C # byte lengths and [[:print:]] in ASCII
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
die() { echo "wifi: $*" >&2; exit 1; }
# Root (a foreground `sudo chipper/start.sh` wire-pod) checks the robot's host key against the repo owner's known_hosts.
if ((EUID == 0)) && owner_home=$(getent passwd "$(stat -c %U "$robot_dir")" | cut -d: -f6) && [[ -n $owner_home ]]; then
  ssh_opts+=(-o "UserKnownHostsFile=$owner_home/.ssh/known_hosts")
fi

# Remote prelude: $id = wlan0's service for $hex, $d its connman dir (connman runs from /data/lib/connman, compiled
# in), $s the stage dir; connected = the online/ready service ("*AO …" / "*AR …").
prelude='mac=$(tr -d : </sys/class/net/wlan0/address); id=wifi_${mac}_${hex}_managed_psk; d=/data/lib/connman/$id
s=/data/lib/connman-staged j=/run/myvector-wifi-job
busy() { grep -qs ^running $j && kill -0 "$(cat $j.pid 2>/dev/null)" 2>/dev/null; } # a dead job is not busy
connected() { connmanctl services | awk '\''substr($0, 3, 1) ~ /[OR]/ { print $NF }'\''; }'
# Remote apply: clear any stage left by an earlier cut-off add (it may hold a password), refuse the connected
# network, and for a saved one drop connman's in-memory copy (connmanctl config --remove).
apply='busy && { echo busy; exit 0; }
rm -rf $s; connected | grep -qx "$id" && { echo connected; exit 0; }
[ -d "$d" ] && connmanctl config "$id" --remove >/dev/null 2>&1
'

remote() { # remote <hex> <script>; stdin passes through
  local out
  # shellcheck disable=SC2029 # hex expands on the client: validated [0-9a-f]
  out=$(ssh "${ssh_opts[@]}" "root@$ip" "hex=$1; $prelude
$2" 2>&1) || die "SSH to $ip failed: ${out##*$'\n'}"
  printf '%s\n' "$out"
}

result() { # the remote's last line -> message + exit code
  case ${1##*$'\n'} in
    done) echo "ok: $2; it counts from the robot's next boot" ;;
    connected) echo "wifi: the robot is on this network now; it is left as is" >&2; exit 3 ;;
    busy) echo "wifi: a test/switch is running on the robot; try again when it is done" >&2; exit 4 ;;
    *) die "remote step failed: ${1##*$'\n'}" ;;
  esac
}

# esc: a GKeyFile string value (connman's settings format): \ -> \\, every space -> \s.
esc() { local v=${1//\\/\\\\}; printf '%s' "${v// /\\s}"; }

case ${1:-} in
  list)
    (($# == 1)) || die "usage: wifi.sh list"
    remote 0 'for f in /data/lib/connman/wifi_${mac}_*_managed_psk; do grep -qs "^Passphrase=" "$f/settings" && echo "saved $f"; done
connmanctl scan wifi >/dev/null 2>&1; sleep 3 # a fresh "seen" list: results land after the scan returns
connmanctl services | awk '\''{ print "seen " $NF }'\''
connected | sed "s/^/connected /"' </dev/null |
      sed -nE "s#^(saved|seen|connected) .*wifi_[0-9a-f]+_([0-9a-f]+)_managed_psk\$#\1 \2#p" | sort -u
    ;;
  add)
    (($# == 1)) || die "usage: wifi.sh add  (stdin: SSID line, password line)"
    IFS= read -r ssid || die "stdin: no SSID line"
    IFS= read -r pw || [[ -n $pw ]] || die "stdin: no password line"
    ((${#ssid} >= 1 && ${#ssid} <= 32)) || die "SSID: 1 to 32 bytes"
    [[ ! $ssid =~ [[:cntrl:]] ]] || die "SSID: no control characters"
    [[ $pw =~ ^[[:print:]]{8,63}$ || $pw =~ ^[0-9A-Fa-f]{64}$ ]] || die "password: 8 to 63 printable ASCII characters (WPA/WPA2)"
    hex=$(printf '%s' "$ssid" | od -An -v -tx1 | tr -d ' \n')
    # The file body goes over the SSH stdin from printf (a builtin: no argv, no here-string temp file).
    out=$(printf 'Name=%s\nSSID=%s\nFavorite=true\nAutoConnect=true\nPassphrase=%s\nIPv4.method=dhcp\nIPv6.method=auto\nIPv6.privacy=disabled\n' \
      "$(esc "$ssid")" "$hex" "$(esc "$pw")" |
      remote "$hex" "$apply"'umask 077; mkdir -p $s/$id && { echo "[$id]"; cat; } >$s/$id/settings &&
  grep -q "^Passphrase=" $s/$id/settings || { rm -rf $s; echo "staging failed"; exit 0; }
rm -rf $d; mv $s/$id $d; rm -rf $s
grep -q "^Passphrase=" $d/settings 2>/dev/null && echo done || echo "move into $d failed"') || exit
    result "$out" "saved '$ssid'"
    ;;
  forget)
    (($# == 2)) && [[ $2 =~ ^([0-9a-f]{2}){1,32}$ ]] || die "usage: wifi.sh forget <ssid-hex>"
    out=$(remote "$2" '[ -d "$d" ] || { rm -rf $s; echo missing; exit 0; }
'"$apply"'rm -rf $d && echo done' </dev/null) || exit
    [[ ${out##*$'\n'} == missing ]] && { echo "ok: not saved on the robot"; exit 0; }
    result "$out" "forgot it"
    ;;
  test | switch)
    (($# == 2)) && [[ $2 =~ ^([0-9a-f]{2}){1,32}$ ]] || die "usage: wifi.sh $1 <ssid-hex>"
    # The job runs detached (setsid, own fds) so it outlives this SSH session, which the network change cuts.
    out=$(remote "$2" 'busy && { echo busy; exit 0; }
old=$(connected | head -n 1)
[ "$old" = "$id" ] && { echo connected; exit 0; }
grep -qs "^Passphrase=" $d/settings || { echo "not saved on the robot"; exit 0; }
[ -n "$old" ] || { echo "the robot is not online on any network"; exit 0; }
mode='"$1"' ip0=$(ip -4 -o addr show wlan0 | awk "{ split(\$4, a, \"/\"); print a[1]; exit }")
export id old mode ip0 j
rm -f $j.pid; echo "running: $mode starting" >$j
setsid sh -c '\''
echo $$ >$j.pid
name() { connmanctl services "$1" 2>/dev/null | awk -F" = " "\$1 ~ /^ *Name\$/ { print \$2 }"; }
state() { connmanctl services "$1" 2>/dev/null | awk "\$1 == \"State\" { print \$3 }"; }
up() { n=0; while [ $n -lt 25 ]; do case $(state "$1") in online|ready) return 0 ;; failure) return 1 ;; esac; sleep 1; n=$((n + 1)); done; return 1; }
addr() { ip -4 -o addr show wlan0 | awk "{ split(\$4, a, \"/\"); print a[1]; exit }"; }
same() { n=0; until a=$(addr); [ "$a" = "$ip0" ]; do n=$((n + 1)); [ $n -ge 15 ] && return 1; sleep 1; done; } # connman drops + re-adds it
reach() { n=0; until c=$(curl -sk -m 4 -o /dev/null -w "%{http_code}" https://escapepod.local/ok); rc=$?; [ "$c" = 200 ]; do n=$((n + 1)); [ $n -ge 8 ] && return 1; sleep 2; done; }
to=$(name "$id") from=$(name "$old") why=
echo "running: $mode: joining $to" >$j
timeout 30 connmanctl connect "$id" >/dev/null 2>&1
if ! up "$id"; then why="did not connect (wrong password, out of range, or saved since Vector last booted: reboot it first)"
elif ! same; then why="got address ${a:-none}, not $ip0: wire-pod would lose Vector"
elif ! reach; then why="connected, but cannot reach wire-pod (curl https://escapepod.local/ok: exit $rc, HTTP $c)"
fi
sig=$(iw dev wlan0 link 2>/dev/null | awk "\$1 == \"signal:\" { print \$2 }")
if [ -n "$why" ] || [ "$mode" = test ]; then
  echo "running: $mode: going back to $from" >$j
  timeout 30 connmanctl connect "$old" >/dev/null 2>&1
  up "$old" && back="Vector is back on $from" || back="Vector could not get back to $from: it picks a saved network itself, else BLE setup"
fi
if [ -n "$why" ]; then echo "$id $(date +%s)" >>$j.fail; echo "failed: $to $why. $back." >$j
elif [ "$mode" = test ]; then echo "ok: $to works (signal $sig dBm, same address $ip0). $back." >$j
else echo "ok: Vector is on $to now (signal $sig dBm, same address $ip0)." >$j
fi
'\'' </dev/null >/dev/null 2>&1 &
echo $! >$j.pid
echo started' </dev/null) || exit
    case ${out##*$'\n'} in
      started) echo "ok: $1 started" ;;
      *) result "$out" "" ;;
    esac
    ;;
  roam)
    (($# == 1)) || die "usage: wifi.sh roam"
    out=$(remote 0 'busy && exit 0
old=$(connected | head -n 1); [ -n "$old" ] || exit 0
iw dev wlan0 scan 2>/dev/null | awk "/^BSS/ { s = \"\" } /signal:/ { s = int(\$2) } /^[ \t]*SSID: / { sub(/^[ \t]*SSID: /, \"\"); if (s != \"\" && \$0 != \"\") print s, \$0 }" >$j.scan
sig() { awk -v n="$1" "{ d = \$1; sub(/^[^ ]+ /, \"\"); if (\$0 == n && (m == \"\" || d + 0 > m + 0)) m = d } END { print m }" $j.scan; }
name() { connmanctl services "$1" 2>/dev/null | awk -F" = " "\$1 ~ /^ *Name\$/ { print \$2 }"; }
cur=$(sig "$(name "$old")") bd=-999 best=
for f in /data/lib/connman/wifi_${mac}_*_managed_psk; do
  id=${f##*/}; [ "$id" = "$old" ] && continue; grep -qs "^Passphrase=" $f/settings || continue
  awk -v id="$id" -v now="$(date +%s)" "\$1 == id && now - \$2 < 3600 { f = 1 } END { exit !f }" $j.fail 2>/dev/null && continue
  nm=$(name "$id"); [ -n "$nm" ] || continue # out of range: no connman service (and "" would match a hidden SSID)
  v=$(sig "$nm"); [ -n "$v" ] && [ "$v" -gt "$bd" ] && bd=$v best=$id
done
[ -n "$best" ] && [ -n "$cur" ] && [ "$bd" -ge $((cur + 8)) ] && echo "roam $best $cur $bd"; exit 0' </dev/null) || exit
    [[ ${out##*$'\n'} =~ ^roam\ wifi_[0-9a-f]+_([0-9a-f]+)_managed_psk\ (-?[0-9]+)\ (-?[0-9]+)$ ]] || exit 0
    echo "ok: roam: a saved network is stronger (${BASH_REMATCH[3]} vs ${BASH_REMATCH[2]} dBm)"
    exec "$0" switch "${BASH_REMATCH[1]}"
    ;;
  status)
    (($# == 1)) || die "usage: wifi.sh status"
    out=$(remote 0 'if grep -qs ^running $j && ! busy; then echo "failed: the job stopped without a result; Refresh shows the network Vector is on"
else cat $j 2>/dev/null || echo none; fi' </dev/null) || exit
    printf '%s\n' "${out##*$'\n'}"
    ;;
  *) die "usage: wifi.sh list | add | forget <ssid-hex> | test|switch <ssid-hex> | status | roam" ;;
esac

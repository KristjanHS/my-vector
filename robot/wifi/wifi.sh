#!/usr/bin/env bash
# shellcheck disable=SC2016 # the single-quoted snippets expand on the robot
# The robot's saved WiFi networks (connman, /data/lib/connman/<service>/settings: root-only 0700/0600, survives make ship).
# connman connects at boot to the strongest saved network in range; the firmware forgets one only on a BLE "forget".
# Usage: wifi.sh list               lines "saved|seen|connected <ssid-hex>" (seen = in range now)
#        wifi.sh add                stdin: line 1 SSID, line 2 password (WPA/WPA2-PSK) — never argv, never a file on this PC
#        wifi.sh forget <ssid-hex>
#   add/forget only change connman's files and never restart connman: the robot stays online, and the change
#   counts from its next boot (a restart can get a new DHCP address, so the robot looks offline: 2026-11-07).
#   Re-saving a saved network drops connman's in-memory copy (else it saves the old password back): until the next
#   boot connman will not fail over to that one. list counts a network as saved only while its file holds a password.
#   The connected network is refused (a wrong password would strand the robot until BLE setup). wire-pod's WiFi page runs this.
#   Exit 0 = done; 1 = bad input or SSH failed; 2 = SETUP (robot/env.sh); 3 = refused: the robot is on that network now.
set -uo pipefail
export LC_ALL=C # byte lengths and [[:print:]] in ASCII
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
die() { echo "wifi: $*" >&2; exit 1; }
# Root (wire-pod's service, HOME=/root) checks the robot's host key against the repo owner's known_hosts.
if ((EUID == 0)) && owner_home=$(getent passwd "$(stat -c %U "$robot_dir")" | cut -d: -f6) && [[ -n $owner_home ]]; then
  ssh_opts+=(-o "UserKnownHostsFile=$owner_home/.ssh/known_hosts")
fi

# Remote prelude: $id = wlan0's service for $hex, $d its connman dir (connman runs from /data/lib/connman, compiled
# in), $s the stage dir; connected = the online/ready service ("*AO …" / "*AR …").
prelude='mac=$(tr -d : </sys/class/net/wlan0/address); id=wifi_${mac}_${hex}_managed_psk; d=/data/lib/connman/$id
s=/data/lib/connman-staged
connected() { connmanctl services | awk '\''substr($0, 3, 1) ~ /[OR]/ { print $NF }'\''; }'
# Remote apply: clear any stage left by an earlier cut-off add (it may hold a password), refuse the connected
# network, and for a saved one drop connman's in-memory copy (connmanctl config --remove).
apply='rm -rf $s; connected | grep -qx "$id" && { echo connected; exit 0; }
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
  *) die "usage: wifi.sh list | add | forget <ssid-hex>" ;;
esac

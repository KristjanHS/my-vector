#!/usr/bin/env bash
# shellcheck disable=SC2016 # the single-quoted snippets expand on the robot
# The robot's saved WiFi networks (connman, /data/lib/connman/<service>/settings: root-only 0700/0600, survives make ship).
# connman connects at boot to the strongest saved network in range; the firmware forgets one only on a BLE "forget".
# Usage: wifi.sh list               lines "saved|seen|connected <ssid-hex>" (seen = in range now)
#        wifi.sh add                stdin: line 1 SSID, line 2 password (WPA/WPA2-PSK) — never argv, never a file on this PC
#        wifi.sh forget <ssid-hex>
#   add/forget change the files with connman stopped (its in-memory copy would overwrite them), then start it and
#   the services it took down: the robot's WiFi drops for ~10 s and reconnects to the strongest saved network. The connected network is
#   refused (a wrong password would strand the robot until BLE setup). wire-pod's WiFi page runs this.
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

# Remote prelude: $id = wlan0's service for $hex; connected = the online/ready service ("*AO …" / "*AR …").
prelude='mac=$(tr -d : </sys/class/net/wlan0/address); id=wifi_${mac}_${hex}_managed_psk; d=/data/lib/connman/$id
connected() { connmanctl services | awk '\''substr($0, 3, 1) ~ /[OR]/ { print $NF }'\''; }'
# Remote apply: refuse the connected one, stage under connman-staged (0700), then detach (the SSH rides the WiFi
# that is about to drop): stop connman, run $change, start it. connman runs from /data/lib/connman (compiled in).
# A job.sh under 2 min old is a change still running: busy. Otherwise the stage dir is cleared (a request cut off
# mid-add leaves no staged password behind).
apply='connected | grep -qx "$id" && { echo connected; exit 0; }
umask 077; s=/data/lib/connman-staged
[ -f $s/job.sh ] && [ $(($(date +%s) - $(stat -c %Y $s/job.sh))) -lt 120 ] && { echo busy; exit 0; }
rm -rf $s; mkdir -p "$s/$id" || { echo "staging failed"; exit 0; }
'
# Stopping connman also stops the units that Require it (avahi-daemon, …): the job starts every service that was
# active before again. $change runs only on a complete stage, so a failed stage never deletes a saved network.
detach='cat >$s/job.sh <<"JOB"
sleep 1
up=$(systemctl list-units --type=service --state=active --plain --no-legend | cut -d" " -f1)
systemctl stop connman
sh -c "$CHANGE"
systemctl start connman || { sleep 3; systemctl start connman; }
for u in $up; do systemctl is-active -q "$u" || systemctl start "$u"; done
rm -rf "$S"
JOB
CHANGE=$change S=$s setsid sh $s/job.sh </dev/null >/dev/null 2>&1 &
echo scheduled'

remote() { # remote <hex> <script>; stdin passes through
  local out
  # shellcheck disable=SC2029 # hex expands on the client: validated [0-9a-f]
  out=$(ssh "${ssh_opts[@]}" "root@$ip" "hex=$1; $prelude
$2" 2>&1) || die "SSH to $ip failed: ${out##*$'\n'}"
  printf '%s\n' "$out"
}

result() { # the remote's last line -> message + exit code
  case ${1##*$'\n'} in
    scheduled) echo "ok: $2; robot WiFi restarts now (~10 s offline)" ;;
    connected) echo "wifi: the robot is on this network now; it is left as is" >&2; exit 3 ;;
    busy) die "the robot is still applying the last change; try again in a minute" ;;
    *) die "remote step failed: ${1##*$'\n'}" ;;
  esac
}

# esc: a GKeyFile string value (connman's settings format): \ -> \\, every space -> \s.
esc() { local v=${1//\\/\\\\}; printf '%s' "${v// /\\s}"; }

case ${1:-} in
  list)
    (($# == 1)) || die "usage: wifi.sh list"
    remote 0 'for f in /data/lib/connman/wifi_${mac}_*_managed_psk; do [ -f "$f/settings" ] && echo "saved $f"; done
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
      remote "$hex" "$apply"'{ echo "[$id]"; cat; } >"$s/$id/settings" && grep -q "^Passphrase=" "$s/$id/settings" ||
  { rm -rf $s; echo "staging failed"; exit 0; }
change="[ -f $s/$id/settings ] && rm -rf $d && mv $s/$id $d"
'"$detach") || exit
    result "$out" "saved '$ssid'"
    ;;
  forget)
    (($# == 2)) && [[ $2 =~ ^([0-9a-f]{2}){1,32}$ ]] || die "usage: wifi.sh forget <ssid-hex>"
    out=$(remote "$2" "[ -d \"\$d\" ] || { echo missing; exit 0; }
$apply"'change="rm -rf $d"
'"$detach" </dev/null) || exit
    [[ ${out##*$'\n'} == missing ]] && { echo "ok: not saved on the robot"; exit 0; }
    result "$out" "forgot it"
    ;;
  *) die "usage: wifi.sh list | add | forget <ssid-hex>" ;;
esac

#!/usr/bin/env bash
# D12 soak (Stage 2.5): `soak.sh [minutes]` (SOAK_MIN 30), one ssh sample a minute; prints max tailscaled VmRSS + min MemAvailable.
# Exit 1 on VmRSS >= RSS_CAP kB, a vic-* MainPID change, tailscale0 not UP or a failed sample; SOAK_INTERVAL (s) overrides 60 for tests.
set -euo pipefail
silent=1 # robot sound lock: makes no robot sound
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"
SOAK_MIN=30 RSS_CAP=40960
min=${1:-$SOAK_MIN} every=${SOAK_INTERVAL:-60}
[[ $min =~ ^[1-9][0-9]*$ ]] || { echo "usage: soak.sh [minutes]" >&2; exit 1; }
# One line per sample: <VmRSS kB> <MemAvailable kB> <tailscale0 UP 1|0> <unit>=<MainPID>...
# shellcheck disable=SC2016 # expands on the robot
probe='p=$(pidof tailscaled); r=$(awk "/^VmRSS/{print \$2}" /proc/${p:-0}/status 2>/dev/null)
a=$(awk "/^MemAvailable/{print \$2}" /proc/meminfo); ip link show tailscale0 2>/dev/null | grep -q "[<,]UP[,>]" && t=1 || t=0
u=$(for s in $(systemctl list-units --no-legend --plain "vic-*" | awk "{print \$1}"); do printf " %s=%s" "$s" "$(systemctl show -p MainPID --value "$s")"; done)
echo "${r:-0} ${a:-0} $t$u"'
fail() { echo "FAIL: soak: $*" >&2; exit 1; }
max_rss=0 min_avail='' base=''
for ((i = 1; i <= min; i++)); do
  ((i == 1)) || sleep "$every"
  # shellcheck disable=SC2029 # $probe is single-quoted above: it expands on the robot
  line=$(ssh "${ssh_opts[@]}" "root@$ip" "$probe") || fail "sample $i: ssh failed"
  read -r rss avail tun pids <<<"$line"
  [[ $rss =~ ^[0-9]+$ && $avail =~ ^[0-9]+$ && $rss -gt 0 ]] || fail "sample $i: no tailscaled ($line)"
  if ((rss > max_rss)); then max_rss=$rss; fi
  if [[ -z $min_avail ]] || ((avail < min_avail)); then min_avail=$avail; fi
  echo "soak $i/$min: VmRSS ${rss} kB, MemAvailable ${avail} kB, tailscale0 $tun"
  ((rss < RSS_CAP)) || fail "sample $i: tailscaled VmRSS $rss kB >= RSS_CAP $RSS_CAP"
  ((tun == 1)) || fail "sample $i: tailscale0 not UP"
  [[ -n $pids ]] || fail "sample $i: no vic-* units"
  [[ -n $base ]] || base=$pids
  [[ $pids == "$base" ]] || fail "sample $i: vic-* MainPID changed: $base -> $pids"
done
echo "soak OK: $min samples, max VmRSS $max_rss kB (cap $RSS_CAP), min MemAvailable $min_avail kB"

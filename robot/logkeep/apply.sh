#!/usr/bin/env bash
# Keep the robot's syslog across a battery death: syslog-ng also writes /data/log/keep/messages (flash, 10 MB + 1 rotation).
# Usage: robot/logkeep/apply.sh   idempotent; re-run after a WireOS reflash. After a death: ssh … 'cat /data/log/keep/messages*'
set -euo pipefail
main=$(dirname "$(git -C "$(dirname "$0")" rev-parse --path-format=absolute --git-common-dir)")
src=$main/robot/work/wire-os-victor
[[ -s $src/robot_ip.txt && -f $src/robot_sshkey ]] || { echo "FAIL: need $src/robot_ip.txt and robot_sshkey" >&2; exit 2; }

ssh -i "$src/robot_sshkey" -o BatchMode=yes -o ConnectTimeout=8 -o PubkeyAcceptedAlgorithms=+ssh-rsa \
  -o HostKeyAlgorithms=+ssh-rsa "root@$(<"$src/robot_ip.txt")" sh -s <<'EOF'
set -e
c=/etc/syslog-ng/syslog-ng.conf
mkdir -p /data/log/keep
if grep -q d_keep $c; then echo "syslog-ng: d_keep already in $c"; else
  mount -o remount,rw /
  trap 'mount -o remount,ro /' EXIT
  cp $c $c.orig
  cat >>$c <<'CONF'
destination d_keep { file("/data/log/keep/messages" template(t_common) logrotate(enable(yes) size(10MB) rotations(1))); };
log { source(s_src); destination(d_keep); };
CONF
  syslog-ng --syntax-only -f $c 2>/dev/null || { cp $c.orig $c; echo "FAIL: syntax; restored $c"; exit 1; }
  echo "syslog-ng: d_keep added to $c"
fi
systemctl restart syslog-ng@default
tag="my-vector logkeep $$"; logger "$tag"; sleep 2
grep -q "$tag" /data/log/keep/messages && echo "PASS: $tag in /data/log/keep/messages" || { echo "FAIL: $tag not in /data/log/keep/messages"; exit 1; }
EOF

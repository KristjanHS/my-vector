#!/usr/bin/env bash
# Keep the robot's syslog across a battery death: syslog-ng also writes /data/log/keep/messages (flash, 10 MB + 1 rotation).
# Usage: robot/logkeep/apply.sh   idempotent; re-run after a WireOS reflash. After a death: ssh … 'cat /data/log/keep/messages*'
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"

cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" sh -s
set -e
c=/etc/syslog-ng/syslog-ng.conf
mkdir -p /data/log/keep
if grep -q d_keep $c; then echo "syslog-ng: d_keep already in $c"; else
  etc_rw
  cp $c $c.orig
  cat >>$c <<'CONF'
destination d_keep { file("/data/log/keep/messages" template(t_common) logrotate(enable(yes) size(10MB) rotations(1))); };
log { source(s_src); destination(d_keep); };
CONF
  syslog-ng --syntax-only -f $c || { cp $c.orig $c; echo "FAIL: syntax; restored $c"; exit 1; }
  etc_ro
  echo "syslog-ng: d_keep added to $c"
fi
etc_ro_check
syslog-ng --syntax-only -f $c || { echo "FAIL: $c syntax"; exit 1; }
systemctl restart syslog-ng@default
tag="my-vector logkeep $(date +%s)-$$"; logger "$tag"; sleep 2
grep -q "$tag\$" /data/log/keep/messages && echo "PASS: $tag in /data/log/keep/messages" || { echo "FAIL: $tag not in /data/log/keep/messages"; exit 1; }
EOF

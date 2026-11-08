#!/usr/bin/env bash
# Install the robot's /data fill guard: diskguard.sh + a 2-min systemd timer, on the read-only rootfs (/etc).
# Usage: robot/diskguard/apply.sh   idempotent (installs only what differs); settings.sh runs it, so make ship re-installs it after a reflash.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"

guard=$(base64 -w0 "$here/diskguard.sh")
# shellcheck disable=SC2029 # GUARD expands on the client: the point
cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "GUARD=$guard sh -s"
set -e
bin=/etc/myvector/diskguard.sh u=/etc/systemd/system/myvector-diskguard w=/etc/systemd/system/timers.target.wants/myvector-diskguard.timer
svc="[Unit]
Description=my-vector: stop a runaway output capture before it fills /data

[Service]
Type=oneshot
ExecStart=/bin/sh $bin"
tmr="[Unit]
Description=my-vector: /data fill guard every 2 min

[Timer]
OnBootSec=2min
OnUnitActiveSec=2min

[Install]
WantedBy=timers.target"
new=$(echo "$GUARD" | base64 -d)
[ -n "$new" ] || { echo "FAIL: empty diskguard.sh (GUARD)"; exit 1; }
etc_put $bin "$new"
etc_put $u.service "$svc"
etc_put $u.timer "$tmr"
if [ -z "$ETC_CHANGED" ] && [ -L "$ETC_ROOT$w" ]; then
  echo "diskguard: already installed"
else
  etc_rw
  systemctl daemon-reload
  systemctl enable myvector-diskguard.timer 2>/dev/null
  etc_ro
  echo "diskguard: installed $bin + myvector-diskguard.timer"
fi
etc_ro_check
systemctl start myvector-diskguard.timer
systemctl start myvector-diskguard.service || { echo "FAIL: myvector-diskguard.service run failed"; exit 1; }
systemctl is-active -q myvector-diskguard.timer || { echo "FAIL: myvector-diskguard.timer not active"; exit 1; }
echo "PASS: diskguard timer active, a run succeeded"
EOF

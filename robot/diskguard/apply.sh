#!/usr/bin/env bash
# Install the robot's /data fill guard: diskguard.sh + a 2-min systemd timer, on the read-only rootfs (/etc).
# Usage: robot/diskguard/apply.sh   idempotent (installs only what differs); settings.sh runs it, so make ship re-installs it after a reflash.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"

guard=$(base64 -w0 "$here/diskguard.sh")
# shellcheck disable=SC2029 # GUARD expands on the client: the point
ssh "${ssh_opts[@]}" "root@$ip" "GUARD=$guard sh -s" <<'EOF'
set -e
bin=/etc/myvector/diskguard.sh u=/etc/systemd/system/myvector-diskguard
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
if [ "$(cat $bin 2>/dev/null)" = "$new" ] && [ "$(cat $u.service 2>/dev/null)" = "$svc" ] &&
  [ "$(cat $u.timer 2>/dev/null)" = "$tmr" ] && [ -L /etc/systemd/system/timers.target.wants/myvector-diskguard.timer ]; then
  echo "diskguard: already installed"
else
  mount -o remount,rw /
  trap 'mount -o remount,ro /' EXIT
  mkdir -p /etc/myvector
  printf '%s\n' "$new" >$bin
  printf '%s\n' "$svc" >$u.service
  printf '%s\n' "$tmr" >$u.timer
  systemctl daemon-reload
  systemctl enable myvector-diskguard.timer 2>/dev/null
  trap - EXIT; mount -o remount,ro /
  echo "diskguard: installed $bin + myvector-diskguard.timer"
fi
awk '$2 == "/" && $4 ~ /^ro(,|$)/ { ro = 1 } END { exit !ro }' /proc/mounts || { echo "FAIL: / is not read-only"; exit 1; }
systemctl start myvector-diskguard.timer
systemctl start myvector-diskguard.service || { echo "FAIL: myvector-diskguard.service run failed"; exit 1; }
systemctl is-active -q myvector-diskguard.timer || { echo "FAIL: myvector-diskguard.timer not active"; exit 1; }
echo "PASS: diskguard timer active, a run succeeded"
EOF

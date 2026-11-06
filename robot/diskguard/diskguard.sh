#!/bin/sh
# Runs ON the robot every 2 min (myvector-diskguard.timer; installed by robot/diskguard/apply.sh).
# Stops a runaway vic-anim output capture (SetWriteAudioOutputCapture left on) before it fills /data:
# 2026-11-05 one capture grew to 822 MB, /data hit 100 % and syslog-ng stalled. Probe captures are seconds long (~64 KB/s).
d=/data/data/com.anki.victor/cache/sound
max_kb=${MAX_KB:-51200} # 50 MB = ~13 min of capture
for f in "$d"/*.wav; do
  [ -f "$f" ] || continue
  kb=$(du -k "$f" | cut -f1)
  [ "$kb" -gt "$max_kb" ] || continue
  : >"$f" # truncate: frees the blocks even while vic-anim writes on (the file goes sparse; du counts real blocks, so a later run catches it)
  # Unlink only once capture is confirmed off: an open, unlinked file would keep filling /data where no glob or find can see it.
  if curl -sf -m 5 -o /dev/null 'http://127.0.0.1:8889/consolefunccall?func=SetWriteAudioOutputCapture&args=false'; then
    sleep 2; : >"$f"; rm -f "$f"
    logger -t myvector-diskguard "removed $f ($kb KB > $max_kb KB): output capture turned off"
  else
    logger -t myvector-diskguard "WARN: capture-off call failed; truncated $f ($kb KB), left in place"
  fi
done
use=$(df -P /data | awk 'NR == 2 { sub("%", "", $5); print $5 }')
[ "${use:-0}" -lt 90 ] || logger -t myvector-diskguard "WARN: /data ${use}% used"

#!/usr/bin/env bash
# Robot logs: syslog-ng also writes /data/log/keep/messages (flash, 10 MB + 1 rotation, survives a battery death); RAM caps: /var/log/messages 8 MB + 1, runtime journal 16 MB.
# Usage: robot/logkeep/apply.sh   idempotent; settings.sh runs it, so make ship re-applies it after a reflash. After a death: ssh … 'cat /data/log/keep/messages*'
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound (shut-up-sleep G2)
. "$here/../env.sh"

cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" sh -s
set -e
c=/etc/syslog-ng/syslog-ng.conf j=/etc/systemd/journald.conf.d/99-myvector.conf jold=/etc/systemd/journald.conf.d/myvector.conf
t=$ETC_ROOT/run/myvector/syslog-ng.conf.new R=$ETC_ROOT
# 8 MB ≈ 10 days at idle (566 B/min, 2026-11-09); a 62 lines/s spam rotates it instead of filling the 474 MB RAM (107 MB seen 2026-11-09).
old='destination d_messages { file("/var/log/messages" template(t_common)); };'
capped='destination d_messages { file("/var/log/messages" template(t_common) logrotate(enable(yes) size(8MB) rotations(1))); };'
# journald: vendor /usr/lib 00-systemd-conf.conf has RuntimeMaxUse=64M; volatile: a /var/log/journal would escape RuntimeMaxUse.
jconf='[Journal]
Storage=volatile
RuntimeMaxUse=16M
RuntimeMaxFileSize=4M'
mkdir -p $R/data/log/keep $R/run/myvector
new=$(awk -v o="$old" -v n="$capped" '$0 == o { $0 = n } 1' $R$c)
grep -q d_keep $R$c || new="$new
destination d_keep { file(\"/data/log/keep/messages\" template(t_common) logrotate(enable(yes) size(10MB) rotations(1))); };
log { source(s_src); destination(d_keep); };"
printf '%s\n' "$new" | grep -qxF "$capped" || { echo "FAIL: no d_messages /var/log/messages line in $c to cap"; exit 1; }
printf '%s\n' "$new" >$t
o=$(syslog-ng --syntax-only -f $t 2>&1) || { rm -f $t; echo "$o"; echo "FAIL: syntax of new $c; not written"; exit 1; }
rm -f $t
# stale <unit> <file>: the unit started before the file's last write (a run that died between write and restart).
# +2 s: the start estimate floors three times (date, uptime, µs), up to ~2 s early.
stale() {
  m=$(systemctl show -p ActiveEnterTimestampMonotonic "$1" | cut -d= -f2) up=$(cut -d' ' -f1 $R/proc/uptime)
  [ "$(stat -c %Y $R$2)" -gt $(($(date +%s) - ${up%.*} + ${m:-0} / 1000000 + 2)) ]
}
[ -f $R$c.orig ] || { etc_rw; cp -p $R$c $R$c.orig; }
etc_put $c "$new"; sl=$ETC_CHANGED ETC_CHANGED=
etc_put $j "$jconf"; jd=$ETC_CHANGED
[ ! -e $R$jold ] || { etc_rw; rm -f $R$jold; jd=1; }
etc_ro
etc_ro_check
if [ -n "$jd" ] || stale systemd-journald $j; then systemctl restart systemd-journald; echo "journald: Storage=volatile RuntimeMaxUse=16M RuntimeMaxFileSize=4M set in $j"
else echo "journald: already capped in $j"; fi
if [ -n "$sl" ] || stale syslog-ng@default $c; then systemctl restart syslog-ng@default; echo "syslog-ng: /var/log/messages 8MB+1 + d_keep set in $c"
else echo "syslog-ng: already capped in $c"; fi
systemctl is-active -q syslog-ng@default || { echo "FAIL: syslog-ng@default not active"; exit 1; }
tag="my-vector logkeep $(date +%s)-$$"; logger "$tag"; sleep 2
for f in /var/log/messages /data/log/keep/messages; do
  grep -q "$tag\$" $R$f || { echo "FAIL: $tag not in $f"; exit 1; }
done
[ -d $R/run/log/journal ] || { echo "FAIL: no /run/log/journal: journald keeps no runtime journal, RuntimeMaxUse caps nothing (Storage= in $j?)"; exit 1; }
kb=$(du -sk $R/run/log/journal | cut -f1)
[ "$kb" -le 17408 ] || { echo "FAIL: runtime journal ${kb} KB > 16 MB cap + 1 MB"; exit 1; }
echo "PASS: $tag in /var/log/messages + /data/log/keep/messages; runtime journal ${kb} KB"
EOF

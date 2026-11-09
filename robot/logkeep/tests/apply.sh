#!/usr/bin/env bash
# Host test of robot/logkeep/apply.sh (make check): its robot half under sh -e against a temp ETC_ROOT, fake mount/systemctl/syslog-ng/logger on PATH.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/logkeep-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: logkeep host test: $*" >&2; exit 1; }

mkdir -p "$tmp/bin"
cat >"$tmp/bin/mount" <<'EOF'
#!/bin/sh
echo "mount $*" >>"$ETC_ROOT/calls"
case $* in *remount,rw*) m=rw ;; *) m=ro ;; esac
echo "/dev/root / ext4 $m 0 0" >"$ETC_ROOT/proc/mounts"
EOF
# FAKE_MONO: the units' ActiveEnterTimestampMonotonic (µs); proc/uptime says 1000 s, so 2000 s = started after every write.
cat >"$tmp/bin/systemctl" <<'EOF'
#!/bin/sh
case $1 in show) echo "ActiveEnterTimestampMonotonic=${FAKE_MONO:-2000000000}" ;; is-active) ;; *) echo "systemctl $*" >>"$ETC_ROOT/calls" ;; esac
EOF
cat >"$tmp/bin/syslog-ng" <<'EOF'
#!/bin/sh
[ -z "$FAKE_SYNTAX_BAD" ] || { echo "syntax error"; exit 1; }
EOF
cat >"$tmp/bin/logger" <<'EOF'
#!/bin/sh
echo "11-09 12:00:00.000 notice root 1 $*" | tee -a "$ETC_ROOT/var/log/messages" >>"$ETC_ROOT/data/log/keep/messages"
EOF
printf '#!/bin/sh\n' >"$tmp/bin/sleep"
chmod +x "$tmp/bin/"*
body=$(sed -n "/<<'EOF' |/,/^EOF\$/{/<<'EOF' |/d;/^EOF\$/d;p}" "$robot/logkeep/apply.sh")
vendor='destination d_messages { file("/var/log/messages" template(t_common)); };'
c=/etc/syslog-ng/syslog-ng.conf jd=/etc/systemd/journald.conf.d
# fresh <name> <d_messages line>: a fake robot root (/ ro, a vendor-shaped syslog-ng.conf, the old myvector.conf drop-in); prints its path
fresh() {
  local r=$tmp/$1; mkdir -p "$r/proc" "$r/etc/syslog-ng" "$r$jd" "$r/var/log" "$r/run/log/journal"
  echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"; echo "1000.00 900.00" >"$r/proc/uptime"
  printf '%s\n' '@version: 4.11' 'source s_src { system(); internal(); };' "$2" 'log { source(s_src); destination(d_messages); };' >"$r$c"
  printf '[Journal]\nRuntimeMaxUse=16M\n' >"$r$jd/myvector.conf"
  echo "$r"
}
run() { (cat "$robot/lib/etc.sh"; echo "$body") | ETC_ROOT=$1 PATH="$tmp/bin:$PATH" sh -e; }

r=$(fresh vendor "$vendor")
out=$(run "$r") || die "vendor run: $out"
grep -qxF 'destination d_messages { file("/var/log/messages" template(t_common) logrotate(enable(yes) size(8MB) rotations(1))); };' "$r$c" ||
  die "d_messages not capped: $(grep d_messages "$r$c")"
grep -q '^destination d_keep ' "$r$c" && cmp -s "$r$c.orig" <(printf '%s\n' '@version: 4.11' 'source s_src { system(); internal(); };' "$vendor" 'log { source(s_src); destination(d_messages); };') ||
  die "no d_keep, or $c.orig is not the vendor file"
grep -qx 'Storage=volatile' "$r$jd/99-myvector.conf" && [[ ! -e $r$jd/myvector.conf ]] || die "journald drop-in: $(ls "$r$jd")"
grep -q 'restart systemd-journald' "$r/calls" && grep -q 'restart syslog-ng@default' "$r/calls" && grep -q ' ro ' "$r/proc/mounts" ||
  die "vendor run restarts/mount: $(cat "$r/calls")"
[[ $out == *PASS:* && ! -e $r/run/myvector/syslog-ng.conf.new ]] || die "vendor run output or temp conf left: $out"

: >"$r/calls"
out=$(run "$r") || die "idle run: $out"
[[ ! -s $r/calls && $out == *"journald: already capped"*"syslog-ng: already capped"*PASS:* ]] || die "idle run wrote or restarted: [$out] calls=[$(cat "$r/calls")]"

touch "$r$c" "$r$jd/99-myvector.conf"
out=$(FAKE_MONO=999700000 run "$r") || die "just-restarted run: $out"
[[ ! -s $r/calls ]] || die "daemons restarted 0.3 s after the conf write were restarted again: $(cat "$r/calls")"

out=$(FAKE_MONO=1 run "$r") || die "stale run: $out"
[[ $(grep -c restart "$r/calls") == 2 ]] && ! grep -q mount "$r/calls" || die "daemons started before the conf write not restarted: $(cat "$r/calls")"

r=$(fresh variant 'destination d_messages { file("/var/log/messages" template(t_common) owner(root)); };'); cp "$r$c" "$tmp/variant.conf"
if out=$(run "$r"); then die "an unknown d_messages variant passed: $out"; fi
[[ $out == *"FAIL: no d_messages"* && ! -e $r/calls && ! -e $r$jd/99-myvector.conf && ! -e $r$c.orig ]] && cmp -s "$r$c" "$tmp/variant.conf" ||
  die "unknown variant: [$out] or it wrote: $(cat "$r/calls" 2>/dev/null)"

r=$(fresh syntax "$vendor"); cp "$r$c" "$tmp/syntax.conf"
if out=$(FAKE_SYNTAX_BAD=1 run "$r"); then die "a syntax failure passed: $out"; fi
[[ $out == *"FAIL: syntax"* && ! -e $r/calls && ! -e $r/run/myvector/syslog-ng.conf.new ]] && cmp -s "$r$c" "$tmp/syntax.conf" ||
  die "syntax failure: [$out], wrote or left the temp conf"

r=$(fresh nojournal "$vendor"); rmdir "$r/run/log/journal"
if out=$(run "$r"); then die "no /run/log/journal passed: $out"; fi
[[ $out == *"FAIL: no /run/log/journal"* ]] || die "no /run/log/journal: $out"
echo "logkeep host test OK"

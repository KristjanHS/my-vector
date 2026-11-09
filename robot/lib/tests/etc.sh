#!/usr/bin/env bash
# Host test of robot/lib/etc.sh (make check): a temp ETC_ROOT plus fake mount/systemctl on PATH that log their calls.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/etc-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: etc.sh host test: $*" >&2; exit 1; }
lib=$robot/lib/etc.sh

mkdir -p "$tmp/bin"
cat >"$tmp/bin/mount" <<'EOF'
#!/bin/sh
echo "mount $*" >>"$ETC_ROOT/calls"
case $* in *remount,rw*) m=rw ;; *) m=ro ;; esac
echo "/dev/root / ext4 $m 0 0" >"$ETC_ROOT/proc/mounts"
EOF
cat >"$tmp/bin/systemctl" <<'EOF'
#!/bin/sh
echo "systemctl $*" >>"$ETC_ROOT/calls"
[ "$1" = enable ] || exit 0
mkdir -p "$ETC_ROOT/etc/systemd/system/timers.target.wants"
ln -sf "/etc/systemd/system/$2" "$ETC_ROOT/etc/systemd/system/timers.target.wants/$2"
EOF
chmod +x "$tmp/bin/"*
# shellcheck source=robot/lib/tests/fakeclock.sh
. "$here/fakeclock.sh"; fakeclock_install "$tmp/bin" # arm timers park until fakeclock_fire
# fresh <name>: a new fake root with / mounted ro; prints its path
fresh() { mkdir -p "$tmp/$1/proc"; echo "/dev/root / ext4 ro 0 0" >"$tmp/$1/proc/mounts"; echo "$tmp/$1"; }
# run <root> <script>: the lib then the script, under sh -e with the fakes first on PATH
run() { (cat "$lib"; echo "$2") | ETC_ROOT=$1 ETC_ARM_SCALE=1 PATH="$tmp/bin:$PATH" sh -e; }

r=$(fresh same); mkdir -p "$r/etc"; echo hi >"$r/etc/f"; chmod 444 "$r/etc/f"
out=$(run "$r" 'etc_put /etc/f hi')
[[ -z $out && ! -e $r/calls ]] || die "etc_put with the same content wrote or remounted: ${out}; $(cat "$r/calls" 2>/dev/null)"

r=$(fresh failw); mkdir -p "$r/etc"; : >"$r/etc/notadir"
if run "$r" 'etc_put /etc/notadir/f x' 2>/dev/null; then die "etc_put under a file succeeded"; fi
grep -q ' ro ' "$r/proc/mounts" || die "a failed write left / $(cat "$r/proc/mounts")"

r=$(fresh arm)
run "$r" "arm late 1 'touch $r/restored'; arm ok 1 'touch $r/restored-ok'; confirm ok"
fakeclock_fire
[[ -e $r/restored ]] || die "arm without confirm: restore did not run"
[[ ! -e $r/restored-ok ]] || die "confirm within the window: restore still ran"

r=$(fresh diskguard)
body=$(sed -n "/<<'EOF' |/,/^EOF\$/{/<<'EOF' |/d;/^EOF\$/d;p}" "$robot/diskguard/apply.sh")
guard=$(base64 -w0 "$robot/diskguard/diskguard.sh")
out1=$(GUARD=$guard run "$r" "$body") || die "diskguard 1st run: $out1"
cmp -s "$r/etc/myvector/diskguard.sh" "$robot/diskguard/diskguard.sh" || die "diskguard.sh not installed byte-equal"
out2=$(GUARD=$guard run "$r" "$body") || die "diskguard 2nd run: $out2"
[[ $out1 == *"diskguard: installed"* && $out2 == "diskguard: already installed"$'\n'"PASS: "* ]] ||
  die "diskguard runs: 1st=[$out1] 2nd=[$out2]"
tmr=/etc/systemd/system/myvector-diskguard.timer
echo stale >"$r$tmr"; : >"$r/calls"
out3=$(GUARD=$guard run "$r" "$body") || die "diskguard upgrade run: $out3"
[[ $out3 == *"changed $tmr"*"diskguard: installed"* ]] && grep -q 'daemon-reload' "$r/calls" && grep -q ' ro ' "$r/proc/mounts" ||
  die "diskguard upgrade (unit changed, wants link present): [$out3] calls=[$(cat "$r/calls")]"
if out4=$(GUARD=bad! run "$r" "$body" 2>&1); then die "diskguard with an undecodable GUARD passed: $out4"; fi
cmp -s "$r/etc/myvector/diskguard.sh" "$robot/diskguard/diskguard.sh" || die "an undecodable GUARD overwrote diskguard.sh"
echo "etc.sh host test OK"

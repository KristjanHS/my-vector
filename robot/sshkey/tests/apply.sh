#!/usr/bin/env bash
# Host test of robot/sshkey/apply.sh (make check): a fake ssh runs the robot half locally against a temp ETC_ROOT and admits a key only if its blob is in an authorized_keys there.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/sshkey-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: sshkey host test: $*" >&2; exit 1; }

mkdir -p "$tmp/bin" "$tmp/keys"
k=$tmp/keys
ssh-keygen -q -t ed25519 -N '' -C victor-dev@anki -f "$k/old" && rm "$k/old.pub" # like the clone: no .pub
ssh-keygen -q -t ed25519 -N '' -C new -f "$k/new"
cp "$k/new" "$k/nopub"
oldb=$(ssh-keygen -y -f "$k/old" | awk '{ print $2 }') newb=$(awk '{ print $2 }' "$k/new.pub")
cat >"$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
k=
while (($#)); do case $1 in -i) k=$2; shift 2 ;; -o) shift 2 ;; root@*) shift; break ;; *) shift ;; esac; done
cmd=$* name=$(basename "$k") b=$(ssh-keygen -y -f "$k" | awk '{ print $2 }')
ph=$(sed -n 's/^PHASE=\([a-z]*\) .*/\1/p' <<<"$cmd"); ph=${ph:-$cmd}
if [[ $b != "${FAKE_KEEP:-}" ]] && { [[ $b == "${FAKE_REJECT:-}" ]] ||
  ! grep -qF -- "$b" <<<"$(cat "$ETC_ROOT/etc/ssh/authorized_keys" "$ETC_ROOT/root/.ssh/authorized_keys" 2>/dev/null)"; }; then
  echo "ssh $name $ph refused" >>"$ETC_ROOT/calls"; echo "root@robot: Permission denied (publickey)." >&2; exit 255
fi
echo "ssh $name $ph" >>"$ETC_ROOT/calls"
exec sh -c "$cmd"
EOF
cat >"$tmp/bin/sshd" <<'EOF'
#!/bin/sh
printf '%s\n' "authorizedkeysfile .ssh/authorized_keys /etc/ssh/authorized_keys" "passwordauthentication ${FAKE_PW:-no}" "kbdinteractiveauthentication no"
EOF
cat >"$tmp/bin/mount" <<'EOF'
#!/bin/sh
echo "mount $*" >>"$ETC_ROOT/calls"
case $* in *remount,rw*) m=rw ;; *) m=ro ;; esac
echo "/dev/root / ext4 $m 0 0" >"$ETC_ROOT/proc/mounts"
EOF
cat >"$tmp/bin/setsid" <<'EOF'
#!/bin/sh
echo "setsid" >>"$ETC_ROOT/calls"
exec "$@"
EOF
chmod +x "$tmp/bin/"*
# shellcheck source=robot/lib/tests/fakeclock.sh
. "$robot/lib/tests/fakeclock.sh"; fakeclock_install "$tmp/bin" # arm timers park until fakeclock_fire
# env_file <key> [old]: a robot.env with ROBOT_KEY=<key>, ROBOT_KEY_OLD=<old> (default old); prints its path
env_file() { printf '%s\n' ROBOT_ESN=0 ROBOT_IP=192.0.2.9 ROBOT_SERVER=x "ROBOT_KEY=$k/$1" "ROBOT_KEY_OLD=$k/${2:-old}" >"$tmp/$1-${2:-old}.env"; echo "$tmp/$1-${2:-old}.env"; }
# fresh <name>: a fake robot root as probed: / ro, the image key only in /etc/ssh/authorized_keys (600), no /root/.ssh; prints its path
fresh() {
  local r=$tmp/$1
  mkdir -p "$r/proc" "$r/etc/ssh" "$r/root"
  echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"
  echo 'root:x:0:0:root:/root:/bin/sh' >"$r/etc/passwd"
  echo 'root:*:19000:0:99999:7:::' >"$r/etc/shadow"
  ssh-keygen -y -f "$k/old" >"$r/etc/ssh/authorized_keys"; chmod 600 "$r/etc/ssh/authorized_keys"
  echo "$r"
}
# run <root> [VAR=value…]: apply.sh with the fakes first on PATH (1 s per arm minute, 1-minute window)
run() { local r=$1; shift; env ETC_ROOT="$r" ETC_ARM_SCALE=1 ROLLBACK_MIN=1 PATH="$tmp/bin:$PATH" ROBOT_ENV="$(env_file new)" "$@" "$robot/sshkey/apply.sh" 2>&1; }
keys() { cat "$1/etc/ssh/authorized_keys" "$1/root/.ssh/authorized_keys" 2>/dev/null || true; }
seq_of() { grep -E '^(ssh|setsid)' "$1/calls" | paste -sd'|'; }

r=$(fresh missing-pub)
rc=0; out=$(run "$r" ROBOT_ENV="$(env_file nopub)") || rc=$?
[[ $rc == 2 && ! -e $r/calls ]] || die "ROBOT_KEY without .pub: rc=$rc out=[$out] calls=[$(cat "$r/calls" 2>/dev/null)]"
r=$(fresh missing-old)
rc=0; out=$(run "$r" ROBOT_ENV="$(env_file new absent)") || rc=$?
[[ $rc == 2 && ! -e $r/calls ]] || die "no image key file: rc=$rc out=[$out]"

r=$(fresh image-key)
rc=0; out=$(run "$r" ROBOT_ENV="$(env_file old)") || rc=$?
[[ $rc == 0 && $out == "WARN: sshkey: ROBOT_KEY is the image key"* && ! -e $r/calls ]] || die "ROBOT_KEY = image key: rc=$rc out=[$out]"

r=$(fresh reject)
rc=0; out=$(run "$r" FAKE_REJECT="$newb") || rc=$?
[[ $rc == 1 && $out == *"new-key login (new connection) is refused"* ]] || die "new key refused: rc=$rc out=[$out]"
[[ $(keys "$r" | grep -cF -- "$oldb") == 1 ]] || die "new key refused: the image key was removed"
grep -q setsid "$r/calls" && die "new key refused: armed a rollback"

r=$(fresh happy)
out=$(run "$r") || die "happy path: $out"
want='ssh new true refused|ssh old add|ssh new true|ssh new swap|setsid|ssh new true|ssh old true refused|ssh new confirm|ssh new check'
[[ $(seq_of "$r") == "$want" ]] || die "happy path order: [$(seq_of "$r")] want [$want]"
[[ $out == *"PASS: sshkey: fresh new-key login; image-key login refused"* && $out == *"PASS: sshkey: image key in no"* ]] || die "happy path output: $out"
grep -qF -- "$oldb" <<<"$(keys "$r")" && die "happy path: the image key is still authorized"
grep -qF -- "$newb" <<<"$(keys "$r")" || die "happy path: the new key is not authorized"
[[ $(stat -c %a "$r/etc/ssh/authorized_keys") == 600 ]] || die "happy path: authorized_keys mode changed"
grep -q ' ro ' "$r/proc/mounts" || die "happy path left / rw"

: >"$r/calls"
out=$(run "$r") || die "2nd run: $out"
[[ $out == *"sshkey: already installed"* ]] && ! grep -q setsid "$r/calls" || die "2nd run: out=[$out] calls=[$(seq_of "$r")]"
rc=0; out=$(run "$r" FAKE_PW=yes) || rc=$?
[[ $rc == 1 && $out == *"FAIL: sshd passwordauthentication yes"* ]] || die "password auth on: rc=$rc out=[$out]"
sed -i 's/^root:\*/root:$6$salt$hash/' "$r/etc/shadow"
rc=0; out=$(run "$r") || rc=$?
[[ $rc == 1 && $out == *"shadow field is not locked"* ]] || die "root password set: rc=$rc out=[$out]"

r=$(fresh oldstays)
rc=0; out=$(run "$r" FAKE_KEEP="$oldb") || rc=$?
[[ $rc == 1 && $out == *"image key still logs in"* ]] && ! grep -q confirm "$r/calls" || die "image key still accepted: rc=$rc out=[$out]"
r2=$(fresh stale) # a failed run's timer, then a re-run inside its window
run "$r2" FAKE_KEEP="$oldb" >/dev/null && die "stale: the failing 1st run passed"
out=$(run "$r2") || die "stale: re-run: $out"
[[ $out == *"sshkey: already installed"* ]] || die "stale: re-run did not see the swap: $out"
fakeclock_fire # the windows end; happy's and stale's confirmed timers must not fire
grep -qF -- "$oldb" <<<"$(keys "$r2")" && die "stale: the earlier run's timer restored the image key after the re-run passed"
[[ $(stat -c %a "$r/etc/ssh/authorized_keys") == 600 ]] || die "no confirm: the restore changed authorized_keys' mode"
grep -qF -- "$newb" <<<"$(keys "$r")" && [[ $(keys "$r" | grep -cF -- "$oldb") == 1 ]] || die "no confirm: rollback did not restore image + new key: $(keys "$r")"
grep -qF -- "$oldb" <<<"$(keys "$tmp/happy")" && die "happy path: the confirmed rollback still fired"
echo "sshkey host test OK"

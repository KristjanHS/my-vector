#!/usr/bin/env bash
# Host test of robot/hostkey/apply.sh (make check): a fake ssh runs the robot half locally against a temp ETC_ROOT and admits a host only if a served host key is in its UserKnownHostsFile.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/hostkey-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: hostkey host test: $*" >&2; exit 1; }
ip=192.0.2.9

mkdir -p "$tmp/bin" "$tmp/keys" "$tmp/image"
k=$tmp/keys
ssh-keygen -q -t ed25519 -N '' -f "$k/new"
ssh-keygen -q -t ed25519 -N '' -f "$k/old"
for t in ecdsa rsa; do ssh-keygen -q -t $t -N '' -C '' -f "$tmp/image/ssh_host_${t}_key" && rm "$tmp/image/ssh_host_${t}_key.pub"; done
cat >"$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
uk=
while (($#)); do case $1 in -o) [[ $2 == UserKnownHostsFile=* ]] && uk=${2#UserKnownHostsFile=}; shift 2 ;; -i) shift 2 ;; root@*) shift; break ;; *) shift ;; esac; done
cmd=$* ph=$(sed -n 's/^PHASE=\([a-z]*\) .*/\1/p' <<<"$cmd"); ph=${ph:-$cmd}
known=$(for f in $uk; do ssh-keygen -F 192.0.2.9 -f "$f" 2>/dev/null; done | awk '!/^#/ { print $3 }')
ok=
for h in "$ETC_ROOT"/etc/ssh/ssh_host_{ecdsa,rsa}_key; do grep -qxF -- "$(ssh-keygen -y -f "$h" | awk '{ print $2 }')" <<<"$known" && ok=1; done
if [[ -z $ok || ( -n ${FAKE_FAIL_KH:-} && $uk == *"/$FAKE_FAIL_KH" ) ]]; then
  echo "ssh $ph hostkey-refused" >>"$ETC_ROOT/calls"; echo "Host key verification failed." >&2; exit 255
fi
echo "ssh $ph" >>"$ETC_ROOT/calls"
exec sh -c "$cmd"
EOF
cat >"$tmp/bin/sshd" <<'EOF'
#!/bin/sh
case $1 in
-T) printf '%s\n' "hostkey /etc/ssh/ssh_host_ecdsa_key" "hostkey /etc/ssh/ssh_host_rsa_key" ;;
-t) exit "${FAKE_SSHD_T:-0}" ;;
esac
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
printf '%s\n' ROBOT_ESN=0 "ROBOT_IP=$ip" ROBOT_SERVER=x "ROBOT_KEY=$k/new" "ROBOT_KEY_OLD=$k/old" >"$tmp/robot.env"
# fresh <name>: a fake robot as probed (/ ro, image host keys 600 with no .pub) + an owner known_hosts holding the image pubs; prints its path
fresh() {
  local r=$tmp/$1 h
  mkdir -p "$r/proc" "$r/etc/ssh" "$r/data"
  echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"
  cp -p "$tmp/image/"* "$r/etc/ssh/"
  for h in "$r"/etc/ssh/ssh_host_*_key; do echo "$ip $(ssh-keygen -y -f "$h" | awk '{ print $1, $2 }')"; done >"$r/kh"
  echo "$r"
}
run() { local r=$1; shift; env ETC_ROOT="$r" ETC_ARM_SCALE=1 ROLLBACK_MIN=1 PATH="$tmp/bin:$PATH" ROBOT_ENV="$tmp/robot.env" \
  HOSTKEY_KNOWN_HOSTS="$r/kh" HOSTKEY_IMAGE="$r/image.known" "$@" "$robot/hostkey/apply.sh" 2>&1; }
is_image() { local t; for t in ecdsa rsa; do cmp -s "$1/etc/ssh/ssh_host_${t}_key" "$tmp/image/ssh_host_${t}_key" || return 1; done; }
is_store() { local t; for t in ecdsa rsa; do cmp -s "$1/etc/ssh/ssh_host_${t}_key" "$1/data/myvector/ssh/ssh_host_${t}_key" || return 1; done; }
pubs_in() { ssh-keygen -F $ip -f "$2" | awk '!/^#/ { print $3 }' | grep -qxF -- "$(awk '{ print $2 }' "$1/data/myvector/ssh/ssh_host_ecdsa_key.pub")"; }

r=$(fresh happy)
out=$(run "$r") || die "happy path: $out"
[[ $out == *"PASS: hostkey: login trusting only the new host keys"* && $out == *"PASS: hostkey: every sshd hostkey"* ]] || die "happy path output: $out"
is_store "$r" || die "happy path: the robot does not serve the stored keys"
[[ $(stat -c %a "$r/etc/ssh/ssh_host_ecdsa_key") == 600 && $(stat -c %a "$r/etc/ssh/ssh_host_rsa_key") == 600 ]] || die "happy path: host key mode not 600"
[[ -s $r/etc/ssh/ssh_host_ecdsa_key.pub ]] || die "happy path: no .pub installed"
pubs_in "$r" "$r/kh" || die "happy path: known_hosts lacks the new keys"
[[ $(wc -l <"$r/kh") == 2 ]] || die "happy path: known_hosts still holds the image keys: $(cat "$r/kh")"
cmp -s <(cut -d' ' -f2- "$r/image.known") <(for t in ecdsa rsa; do ssh-keygen -y -f "$tmp/image/ssh_host_${t}_key" | awk '{ print $1, $2 }'; done) ||
  die "happy path: image.known is not the image pubs"
grep -q setsid "$r/calls" || die "happy path: no rollback armed"
grep -q ' ro ' "$r/proc/mounts" || die "happy path left / rw"

: >"$r/calls"
cp "$r/kh" "$tmp/kh.before"
out=$(run "$r") || die "2nd run: $out"
[[ $out == *"hostkey: already installed"* ]] && ! grep -q setsid "$r/calls" || die "2nd run: out=[$out] calls=[$(cat "$r/calls")]"
cmp -s "$r/kh" "$tmp/kh.before" || die "2nd run changed known_hosts"

r2=$(fresh sshd-t)
cp "$r2/kh" "$tmp/kh.before"
rc=0; out=$(run "$r2" FAKE_SSHD_T=1) || rc=$?
[[ $rc == 1 && $out == *"sshd -t rejects"* ]] || die "sshd -t fails: rc=$rc out=[$out]"
is_image "$r2" && [[ ! -e $r2/etc/ssh/ssh_host_ecdsa_key.pub ]] || die "sshd -t fails: image keys not restored"
cmp -s "$r2/kh" "$tmp/kh.before" || die "sshd -t fails: known_hosts changed"

r3=$(fresh newfail)
cp "$r3/kh" "$tmp/kh.before"
rc=0; out=$(run "$r3" FAKE_FAIL_KH=new) || rc=$?
[[ $rc == 1 && $out == *"image keys restored"* ]] || die "new-pub login fails: rc=$rc out=[$out]"
is_image "$r3" || die "new-pub login fails: image keys not restored at once"
grep -q '^ssh restore$' "$r3/calls" && ! grep -q '^ssh confirm' "$r3/calls" || die "new-pub login fails: calls=[$(cat "$r3/calls")]"
cmp -s "$r3/kh" "$tmp/kh.before" || die "new-pub login fails: known_hosts changed"

cp -p "$tmp/image/"* "$r/etc/ssh/" && rm "$r/etc/ssh/"*.pub # reflash: image keys back, known_hosts = the new keys
cp "$r/kh" "$tmp/kh.before"
out=$(run "$r") || die "reflash: $out"
[[ $out == *"a reflash"* ]] && is_store "$r" || die "reflash: stored keys not re-copied: $out"
cmp -s "$r/kh" "$tmp/kh.before" || die "reflash: known_hosts changed"
cp -p "$tmp/image/"* "$r/etc/ssh/" && mv "$r/image.known" "$r/image.gone"
rc=0; out=$(run "$r") || rc=$?
[[ $rc == 1 && $out == *"no login trusting"* ]] && is_image "$r" || die "reflash, no image.known: rc=$rc out=[$out]"

r4=$(fresh hashed)
ssh-keygen -q -H -f "$r4/kh" >/dev/null 2>&1
out=$(run "$r4") || die "hashed known_hosts: $out"
pubs_in "$r4" "$r4/kh" && ! grep -q "^$ip " "$r4/kh" || die "hashed known_hosts: new lines missing or unhashed: $(cat "$r4/kh")"

sleep 2 # the 1 s window passes; the confirmed timer must not fire
is_store "$r4" || die "the confirmed rollback still fired"
echo "hostkey host test OK"

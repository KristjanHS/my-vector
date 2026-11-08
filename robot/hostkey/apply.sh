#!/usr/bin/env bash
# Give the robot per-robot sshd host keys (plan H1): made once in HOSTKEY_STORE, installed over every `sshd -T` hostkey, proven by a login trusting only them, then the owner's known_hosts follows.
# Usage: robot/hostkey/apply.sh   idempotent (`hostkey: already installed`); when known_hosts refuses (reflash, interrupted run) it logs in trusting HOSTKEY_IMAGE/HOSTKEY_PENDING and re-copies the stored keys only if this laptop approved them. Exit 2 = SETUP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"

store=${HOSTKEY_STORE:-/data/myvector/ssh}
image=${HOSTKEY_IMAGE:-$robot_dir/../secrets/robot-hostkey-image.known}
# NEW keys reported over a trusted login, kept from install until known_hosts trusts them (no lock-out if a run dies or the robot reboots).
pending=${HOSTKEY_PENDING:-$robot_dir/../secrets/robot-hostkey-pending.known}
# The owner's known_hosts: root (a foreground `sudo chipper/start.sh` wire-pod) checks it too (wifi.sh).
kh=${HOSTKEY_KNOWN_HOSTS:-$(getent passwd "$(stat -c %U "$robot_dir")" | cut -d: -f6)/.ssh/known_hosts}
[[ -f $kh ]] || { echo "SETUP: hostkey: no $kh; ssh root@$ip once to record the robot's host key" >&2; exit 2; }
mkdir -p "$robot_dir/work"
tmp=$(mktemp -d "$robot_dir/work/hostkey.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# After a reflash only the image key logs in (sshkey/apply.sh runs next and swaps it).
ids=(-i "$key"); [[ -f $key_old ]] && ids+=(-i "$key_old")
# trust <known_hosts…>: ssh options that accept only the host keys listed there
trust() { opts=("${ssh_base[@]}" -o IdentitiesOnly=yes -o ControlPath=none -o StrictHostKeyChecking=yes -o UpdateHostKeys=no
  -o "UserKnownHostsFile=$1" -o GlobalKnownHostsFile=/dev/null -o KnownHostsCommand=none "${ids[@]}"); }
try() { trust "$1"; ssh "${opts[@]}" "root@$ip" true </dev/null; }
# remote <known_hosts> <phase>: etc.sh + the robot half below, run as PHASE
remote() {
  trust "$1"
  # shellcheck disable=SC2029 # the env assignments expand on the client: the point
  cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${opts[@]}" "root@$ip" "PHASE=$2 STORE=$store sh -s"
set -e
paths() { sshd -T | awk '$1 == "hostkey" { print $2 }'; }
[ -n "$(paths)" ] || { echo "FAIL: sshd -T shows no hostkey"; exit 1; }
S=$ETC_ROOT$STORE B=$ETC_ROOT/run/myvector/hostkey-image
case $PHASE in
make)
  mkdir -p "$S"
  chmod 700 "$S"
  diff=
  for p in $(paths); do
    b=$(basename "$p") t=${p##*/ssh_host_}
    if [ ! -s "$S/$b" ] || [ ! -s "$S/$b.pub" ]; then
      rm -f "$S/$b" "$S/$b.pub"
      ssh-keygen -q -N '' -C '' -t "${t%_key}" -f "$S/$b" </dev/null >/dev/null
      echo "hostkey: made $STORE/$b"
    fi
    echo "CUR $(ssh-keygen -y -f "$ETC_ROOT$p" | awk '{ print $1, $2 }')"
    echo "NEW $(awk '{ print $1, $2 }' "$S/$b.pub")"
    cmp -s "$ETC_ROOT$p" "$S/$b" || diff=1
  done
  if [ -n "$diff" ]; then echo "STATE diff"; else echo "STATE same"; fi
  ;;
install)
  rm -rf "$B"
  mkdir -p "$B"
  r="mount -o remount,rw /;"
  for p in $(paths); do
    b=$(basename "$p")
    [ -s "$S/$b" ] || { echo "FAIL: no $STORE/$b"; exit 1; }
    cp -p "$ETC_ROOT$p" "$B/$b"
    r="$r cp -p '$B/$b' '$ETC_ROOT$p.new' && mv -f '$ETC_ROOT$p.new' '$ETC_ROOT$p';"
    if [ -f "$ETC_ROOT$p.pub" ]; then cp -p "$ETC_ROOT$p.pub" "$B/$b.pub"; r="$r cp -p '$B/$b.pub' '$ETC_ROOT$p.pub';"
    else r="$r rm -f '$ETC_ROOT$p.pub';"; fi
  done
  printf '%s\n' "$r sync; mount -o remount,ro /" >"$B/restore"
  arm hostkey "$ROLLBACK_MIN" "sh '$B/restore'"
  for p in $(paths); do
    b=$(basename "$p")
    etc_put "$p" "$(cat "$S/$b")"
    [ -n "$(find "$ETC_ROOT$p" -perm 600)" ] || { etc_rw; chmod 600 "$ETC_ROOT$p"; }
    etc_put "$p.pub" "$(cat "$S/$b.pub")"
  done
  if ! sshd -t; then
    etc_ro
    sh "$B/restore"
    confirm hostkey
    echo "FAIL: sshd -t rejects the new host keys; image keys restored"
    exit 1
  fi
  etc_ro
  etc_ro_check
  echo "hostkey: new host keys installed; rollback armed ($ROLLBACK_MIN min)"
  ;;
restore)
  [ -f "$B/restore" ] || { echo "FAIL: no $B/restore"; exit 1; }
  sh "$B/restore"
  confirm hostkey
  echo "hostkey: image keys restored"
  ;;
confirm)
  confirm hostkey
  echo "hostkey: confirmed"
  ;;
check)
  for p in $(paths); do
    b=$(basename "$p")
    cmp -s "$ETC_ROOT$p" "$S/$b" || { echo "FAIL: $p is not $STORE/$b"; exit 1; }
    [ -n "$(find "$ETC_ROOT$p" -perm 600)" ] || { echo "FAIL: $p is not mode 600"; exit 1; }
  done
  etc_ro_check
  echo "PASS: hostkey: every sshd hostkey = its $STORE copy, mode 600"
  ;;
*) echo "FAIL: unknown PHASE=$PHASE"; exit 1 ;;
esac
EOF
}

# new_approved: every NEW blob is in $kh's $ip entries or $pending
new_approved() {
  { ssh-keygen -F "$ip" -f "$kh" 2>/dev/null | awk '!/^#/ { print $3 }'; [[ ! -s $pending ]] || awk '{ print $3 }' "$pending"; } >"$tmp/approved"
  ! awk '{ print $3 }' "$tmp/new" | grep -qvxFf "$tmp/approved"
}
# rewrite_kh: $kh's $ip lines become the NEW keys (hashed if $kh hashes); written in place (keeps owner, mode, inode), backup $kh.hostkey-bak
rewrite_kh() {
  local n=$kh.hostkey-new
  cp -p "$kh" "$kh.hostkey-bak" && cp "$kh" "$n" || { echo "FAIL: hostkey: cannot back up $kh next to it" >&2; exit 1; }
  ssh-keygen -q -R "$ip" -f "$n" >/dev/null 2>&1 || { rm -f "$n" "$n.old"; echo "FAIL: hostkey: ssh-keygen -R $ip failed on $n; $kh unchanged" >&2; exit 1; }
  cp "$tmp/new" "$tmp/add"
  if grep -q '^|1|' "$kh"; then
    ssh-keygen -q -H -f "$tmp/add" >/dev/null 2>&1 || { rm -f "$n" "$n.old"; echo "FAIL: hostkey: cannot hash the new $ip lines" >&2; exit 1; }
  fi
  cat "$tmp/add" >>"$n" && cat "$n" >"$kh" || { echo "FAIL: hostkey: writing $kh failed; restore it from $kh.hostkey-bak" >&2; exit 1; }
  rm -f "$n" "$n.old"
  echo "hostkey: $kh: $ip lines = the new host keys (old file: $kh.hostkey-bak)"
}

[[ ! -e $kh.hostkey-new ]] ||
  { echo "FAIL: hostkey: an earlier rewrite of $kh did not finish; restore it from $kh.hostkey-bak, then rm $kh.hostkey-new" >&2; exit 1; }
via=$kh rc=0
out=$(remote "$kh" make 2>"$tmp/err") || rc=$?
if ((rc)); then
  # only an ssh failure (255: host key refused, unreachable) earns the fallback; a robot-side FAIL is shown as is
  ((rc == 255)) || { echo "$out"; cat "$tmp/err" >&2; echo "FAIL: hostkey: the robot half failed (exit $rc)" >&2; exit 1; }
  fb=; for f in "$image" "$pending"; do [[ ! -s $f ]] || fb+="${fb:+ }$f"; done
  [[ -n $fb ]] && out=$(remote "$fb" make 2>>"$tmp/err") ||
    { cat "$tmp/err" >&2; echo "FAIL: hostkey: no login trusting $kh, the saved image keys ($image) or the pending keys ($pending)" >&2; exit 1; }
  via=$fb
  echo "hostkey: $kh refused; logged in trusting $fb (a reflash or an interrupted run)"
fi
grep -v '^\(CUR\|NEW\|STATE\) ' <<<"$out" || true
sed -n "s/^CUR /$ip /p" <<<"$out" >"$tmp/cur"
sed -n "s/^NEW /$ip /p" <<<"$out" >"$tmp/new"
[[ -s $tmp/cur && -s $tmp/new ]] || { echo "FAIL: hostkey: the robot listed no host keys: $out" >&2; exit 1; }
awk 'NF != 3 { exit 1 }' "$tmp/cur" "$tmp/new" || { echo "FAIL: hostkey: a CUR/NEW line is not <type> <key>: $out" >&2; exit 1; }
if [[ $via != "$kh" ]] && ! new_approved; then
  echo "FAIL: hostkey: the robot's stored keys are not ones this laptop approved (store regenerated, or an impostor); check the fingerprint by hand" >&2
  exit 1
fi

if [[ $out == *"STATE same"* ]]; then
  echo "hostkey: already installed"
  remote "$via" confirm >/dev/null || { echo "FAIL: hostkey: confirm failed" >&2; exit 1; } # disarms a failed earlier run's timer
  if [[ $via != "$kh" ]] && ! try "$kh" 2>/dev/null; then rewrite_kh; fi
  remote "$kh" check
  rm -f "$pending"
  exit 0
fi
if [[ $via == "$kh" && ! -s $image ]]; then
  cp "$tmp/cur" "$image" || { echo "FAIL: hostkey: cannot save the image host keys to $image" >&2; exit 1; }
  echo "hostkey: saved the image host keys to $image (reflash recovery)"
fi

cp "$tmp/new" "$pending" || { echo "FAIL: hostkey: cannot save the new host keys to $pending" >&2; exit 1; }
out=$(remote "$via" install) || { echo "$out"; echo "FAIL: hostkey: install failed; image keys restored or the armed rollback restores them" >&2; exit 1; }
echo "$out"
if ! try "$tmp/new" 2>"$tmp/err"; then
  cat "$tmp/err" >&2
  cat "$tmp/cur" "$tmp/new" >"$tmp/both"
  if remote "$tmp/both" restore; then echo "FAIL: hostkey: a login trusting only the new host keys failed; image keys restored" >&2
  else echo "FAIL: hostkey: a login trusting only the new host keys failed; the armed rollback restores the image keys" >&2; fi
  exit 1
fi
echo "PASS: hostkey: login trusting only the new host keys (new connection)"
if ! try "$kh" 2>/dev/null; then rewrite_kh; fi
remote "$kh" confirm || { echo "FAIL: hostkey: confirm over $kh failed; the armed rollback restores the image keys, re-run" >&2; exit 1; }
rm -f "$pending"
remote "$kh" check

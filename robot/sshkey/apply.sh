#!/usr/bin/env bash
# Swap the robot's root SSH key to ROBOT_KEY (plan K3/K4): add it, log in with it, arm a rollback, drop the image key, re-verify, confirm.
# Usage: robot/sshkey/apply.sh   idempotent (`sshkey: already installed`); after a reflash it bootstraps from key_old. Exit 2 = SETUP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
. "$here/../env.sh"

[[ -f $key_old ]] || { echo "SETUP: sshkey: no image key $key_old (ROBOT_KEY_OLD)" >&2; exit 2; }
blob() { if [[ -f $1.pub ]]; then awk '{ print $2; exit }' "$1.pub"; else ssh-keygen -y -P '' -f "$1" | awk '{ print $2; exit }'; fi; }
new=$(blob "$key") && old=$(blob "$key_old") && [[ -n $new && -n $old ]] ||
  { echo "SETUP: sshkey: cannot read the public half of $key or $key_old" >&2; exit 2; }
if [[ $new == "$old" ]]; then
  echo "WARN: sshkey: ROBOT_KEY is the image key (victor-dev); swap skipped — set ROBOT_KEY=backup/myvector_ed25519 in robot/robot.env"
  exit 0
fi
[[ -f $key.pub ]] || { echo "SETUP: sshkey: no $key.pub" >&2; exit 2; }
new64=$(head -1 "$key.pub" | base64 -w0)

opts=("${ssh_base[@]}" -o IdentitiesOnly=yes -o ControlPath=none)
try() { ssh "${opts[@]}" -i "$1" "root@$ip" true </dev/null; }
# remote <key> <phase>: etc.sh + the robot half below, run as PHASE
remote() {
  # shellcheck disable=SC2029 # the env assignments expand on the client: the point
  cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${opts[@]}" -i "$1" "root@$ip" "PHASE=$2 OLD=$old NEW64=$new64 sh -s"
set -e
new=$(echo "$NEW64" | base64 -d)
nb=$(echo "$new" | awk '{ print $2 }')
home=$(awk -F: '$1 == "root" { print $6 }' "$ETC_ROOT/etc/passwd")
# files: every AuthorizedKeysFile path, %h/%u expanded, relative = under root's home
files() {
  sshd -T | awk '$1 == "authorizedkeysfile" { for (i = 2; i <= NF; i++) print $i }' |
    sed "s|%h|$home|g; s|%u|root|g; s|%%|%|g; s|^\([^/]\)|$home/\1|"
}
has() { [ -f "$ETC_ROOT$1" ] && grep -qF -- "$2" "$ETC_ROOT$1"; }
[ -n "$(files)" ] || { echo "FAIL: sshd -T shows no authorizedkeysfile"; exit 1; }
case $PHASE in
add)
  t=
  for f in $(files); do if has "$f" "$OLD"; then t=$f; break; fi; done
  [ -n "$t" ] || { echo "FAIL: the image key is in no AuthorizedKeysFile"; exit 1; }
  has "$t" "$nb" || etc_put "$t" "$(cat "$ETC_ROOT$t")
$new"
  etc_ro
  etc_ro_check
  echo "sshkey: $t = image key + new key"
  ;;
swap)
  ok=
  for f in $(files); do if has "$f" "$nb"; then ok=1; fi; done
  [ -n "$ok" ] || { echo "FAIL: the new key is in no AuthorizedKeysFile; not removing the image key"; exit 1; }
  bak=$ETC_ROOT/run/myvector/sshkey restore= n=0
  for f in $(files); do
    has "$f" "$OLD" || continue
    n=$((n + 1))
    mkdir -p "$bak"
    cp -p "$ETC_ROOT$f" "$bak/$n"
    restore="$restore cp -p '$bak/$n' '$ETC_ROOT$f.new' && mv -f '$ETC_ROOT$f.new' '$ETC_ROOT$f';"
  done
  if [ "$n" -eq 0 ]; then echo "sshkey: already installed"; exit 0; fi
  arm sshkey "$ROLLBACK_MIN" "mount -o remount,rw /;$restore sync; mount -o remount,ro /"
  for f in $(files); do if has "$f" "$OLD"; then etc_put "$f" "$(grep -vF -- "$OLD" "$ETC_ROOT$f" || true)"; fi; done
  etc_ro
  etc_ro_check
  echo "sshkey: image key removed; rollback armed ($ROLLBACK_MIN min)"
  ;;
confirm)
  confirm sshkey
  echo "sshkey: confirmed"
  ;;
check)
  for f in $(files); do if has "$f" "$OLD"; then echo "FAIL: $f still holds the image key"; exit 1; fi; done
  sshd -T | awk '($1 == "passwordauthentication" || $1 == "kbdinteractiveauthentication") && $2 != "no" { print "FAIL: sshd " $1 " " $2; bad = 1 } END { exit bad }'
  case $(awk -F: '$1 == "root" { print substr($2, 1, 1) }' "$ETC_ROOT/etc/shadow") in
  '!' | '*') ;;
  *) echo "FAIL: root's shadow field is not locked (! or *)"; exit 1 ;;
  esac
  etc_ro_check
  echo "PASS: sshkey: image key in no AuthorizedKeysFile; password + kbd-interactive login off; root password locked"
  ;;
*) echo "FAIL: unknown PHASE=$PHASE"; exit 1 ;;
esac
EOF
}

if try "$key" 2>/dev/null; then
  echo "sshkey: new key logs in"
else
  remote "$key_old" add || { echo "FAIL: sshkey: no key logs in, or adding the new key failed" >&2; exit 1; }
  try "$key" || { echo "FAIL: sshkey: a new-key login (new connection) is refused; the image key stays" >&2; exit 1; }
  echo "PASS: sshkey: new-key login (new connection)"
fi
out=$(remote "$key" swap) || { echo "$out"; exit 1; }
echo "$out"
if [[ $out != *"already installed"* ]]; then
  try "$key" || { echo "FAIL: sshkey: fresh new-key login failed; the armed rollback restores image key + new key" >&2; exit 1; }
  if o=$(try "$key_old" 2>&1); then echo "FAIL: sshkey: the image key still logs in; the armed rollback restores image key + new key" >&2; exit 1; fi
  [[ $o == *"Permission denied"* ]] || { echo "FAIL: sshkey: image-key refusal not shown ($o); the armed rollback restores image key + new key" >&2; exit 1; }
  echo "PASS: sshkey: fresh new-key login; image-key login refused"
fi
# Also when already installed: it disarms a failed earlier run's timer (a new-key login just passed on these files).
remote "$key" confirm || { echo "FAIL: sshkey: confirm failed; the armed rollback restores image key + new key" >&2; exit 1; }
remote "$key" check

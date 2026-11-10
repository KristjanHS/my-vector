#!/usr/bin/env bash
# Install tailscaled on the robot as a boot unit (plan D2/D4/D5): the binaries fetch.sh unpacked into <main>/robot/work/tailscale/
# go to TS_DIR when their sha256 differs, TS_UNIT is written + enabled + started, then `tailscale up` runs only while the node is
# not logged in and TS_AUTHKEY exists (else WARN). The key reaches the robot on ssh stdin into /run, never in argv (D4). The unit owns the
# OUTPUT -o tailscale0 ACCEPT pair (D13: base OUTPUT is policy DROP).
# Usage: robot/tailscale/apply.sh   idempotent; settings.sh runs it before netguard (D6). Exit 0 = applied (or WARN printed), 1 = FAIL, 2 = SETUP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=robot/env.sh
silent=1 # robot sound lock: makes no robot sound
. "$here/../env.sh"
main=${robot_dir%/robot}
work=${TS_WORK:-$main/robot/work/tailscale}
authkey=${TS_AUTHKEY:-$main/secrets/tailscale-authkey}
ts_dir=/data/myvector/tailscale run_key=/run/myvector/tailscale-authkey
die() { echo "FAIL: tailscale: $*" >&2; exit 1; }
for b in tailscale tailscaled; do
  [[ -x $work/$b ]] || { echo "SETUP: tailscale: no $work/$b; run robot/tailscale/fetch.sh" >&2; exit 2; }
done

# remote <phase> [VAR=value…]: the robot half below, after lib/etc.sh
remote() {
  local phase=$1; shift
  # shellcheck disable=SC2029 # the VARs expand on the client: the point
  cat "$here/../lib/etc.sh" - <<'EOF' | ssh "${ssh_opts[@]}" "root@$ip" "PHASE=$phase TS_DIR=$ts_dir RUN_KEY=$run_key $* sh -s"
set -e
unit=/etc/systemd/system/myvector-tailscaled.service sock=/run/tailscale/tailscaled.sock
d=$ETC_ROOT$TS_DIR
state() { "$d/tailscale" --socket="$sock" status --json 2>/dev/null | sed -n 's/^[[:space:]]*"BackendState": "\([A-Za-z]*\)".*/\1/p' | head -n 1; }
case $PHASE in
check) # SUMS="<name>=<sha256> …": prints the names whose robot copy differs
  need=
  for kv in $SUMS; do
    [ "$(sha256sum "$d/${kv%%=*}" 2>/dev/null | cut -d' ' -f1)" = "${kv#*=}" ] || need="$need ${kv%%=*}"
  done
  echo "need:$need" ;;
unit) # [NEWBIN=1]: unit per D5, enabled, running; prints state=<BackendState>
  if pidof tailscaled >/dev/null && ! systemctl is-active -q myvector-tailscaled; then
    echo "FAIL: a tailscaled runs outside myvector-tailscaled (hand-run spike?): stop it first"; exit 1
  fi
  etc_put $unit "[Unit]
Description=my-vector tailscaled: tailnet path to the PC (robot/tailscale)
After=network-pre.target myvector-netguard.service
RequiresMountsFor=/data
Wants=mount-data.service
After=mount-data.service

[Service]
Type=notify
RuntimeDirectory=tailscale
ExecStartPre=/bin/sh -c 'iptables -C OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null || iptables -I OUTPUT -o tailscale0 -j ACCEPT'
ExecStartPre=/bin/sh -c 'ip6tables -C OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null || ip6tables -I OUTPUT -o tailscale0 -j ACCEPT'
ExecStart=$TS_DIR/tailscaled --statedir=$TS_DIR/state/ --socket=$sock --port=41641 --tun=tailscale0
ExecStopPost=$TS_DIR/tailscaled --cleanup
ExecStopPost=-/bin/sh -c 'while iptables -D OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null; do :; done'
ExecStopPost=-/bin/sh -c 'while ip6tables -D OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null; do :; done'
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target"
  if [ -n "$ETC_CHANGED" ] || ! systemctl is-enabled -q myvector-tailscaled 2>/dev/null; then
    etc_rw; systemctl daemon-reload; systemctl enable myvector-tailscaled 2>/dev/null; etc_ro
    echo "tailscale: installed myvector-tailscaled.service"
  fi
  etc_ro_check
  if ! systemctl is-active -q myvector-tailscaled; then systemctl start myvector-tailscaled; echo "tailscale: started tailscaled"
  elif [ -n "$ETC_CHANGED" ] || [ -n "${NEWBIN:-}" ]; then systemctl restart myvector-tailscaled; echo "tailscale: restarted tailscaled"
  fi
  i=0 s=$(state) # just started: NoState/Starting is not "logged out"; up to ~15 s to settle
  while { [ -z "$s" ] || [ "$s" = NoState ] || [ "$s" = Starting ]; } && [ $i -lt 15 ]; do sleep 1; i=$((i + 1)); s=$(state); done
  echo "state=$s" ;;
up) # the key is at $RUN_KEY (pushed on stdin); removed whatever up does
  trap 'rm -f "$ETC_ROOT$RUN_KEY"' EXIT
  "$d/tailscale" --socket="$sock" up --auth-key="file:$ETC_ROOT$RUN_KEY" --hostname=vector --advertise-tags=tag:vector \
    --accept-dns=false --netfilter-mode=off
  echo "tailscale: up, state=$(state)" ;;
esac
EOF
}

sums=
for b in tailscale tailscaled; do sums+="$b=$(sha256sum "$work/$b" | cut -d' ' -f1) "; done
out=$(remote check "SUMS='$sums'") || die "check: $out"
newbin=
for b in ${out#need:}; do
  # shellcheck disable=SC2016 # expands on the robot
  ssh "${ssh_opts[@]}" "root@$ip" 'd=${ETC_ROOT:-}'"$ts_dir/$b"'; mkdir -p "${d%/*}" && cat >"$d.new" && chmod 755 "$d.new" && mv -f "$d.new" "$d"' \
    <"$work/$b" || die "push $b failed"
  echo "tailscale: pushed $b"
  newbin=NEWBIN=1
done
out=$(remote unit $newbin) || die "${out:-unit phase failed}"
echo "${out%state=*}" | sed '/^$/d'
state=${out##*state=}
if [[ $state == Running ]]; then echo "tailscale: logged in ($state)"; exit 0; fi
[[ $state == NeedsLogin ]] || { echo "WARN: tailscale: BackendState '${state}' (not Running, not NeedsLogin): key not used"; exit 0; }
[[ -f $authkey ]] || { echo "WARN: tailscale: not logged in (state ${state:-unknown}) and no $authkey — see secrets/README.md"; exit 0; }
# shellcheck disable=SC2016 # expands on the robot
ssh "${ssh_opts[@]}" "root@$ip" 'umask 077; k=${ETC_ROOT:-}'"$run_key"'; mkdir -p "${k%/*}" && cat >"$k"' <"$authkey" || die "key push failed"
out=$(remote up) || { ssh "${ssh_opts[@]}" "root@$ip" "rm -f \${ETC_ROOT:-}$run_key" || :; die "${out:-up phase failed}"; }
echo "$out"

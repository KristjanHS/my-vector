#!/usr/bin/env bash
# Host test of robot/tailscale/apply.sh (make check): ssh runs the robot half locally against ETC_ROOT; the "pushed" fake tailscale
# answers status from $ETC_ROOT/ts-state and copies the key file `up` reads to $ETC_ROOT/seen-key. The session comes from FAKE_SRC
# (default PC_LAN_IP); a tailnet source must not restart a running tailscaled.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
tmp=$(mktemp -d "$main/robot/work/tailscale-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: tailscale apply test: $*" >&2; exit 1; }
unit=etc/systemd/system/myvector-tailscaled.service
mkdir -p "$tmp/bin" "$tmp/work" "$tmp/empty"
cat >"$tmp/bin/ssh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$ETC_ROOT/argv"
while (($#)); do case $1 in -i | -o | -O) shift 2 ;; root@*) shift; break ;; *) shift ;; esac; done
cmd=$*
ph=$(sed -n 's/^PHASE=\([a-z]*\) .*/\1/p' <<<"$cmd"); echo "ssh ${ph:-raw}" >>"$ETC_ROOT/calls"
SSH_CLIENT="${FAKE_SRC:-192.0.2.1} 50000 22" exec sh -c "$cmd"
EOF
cat >"$tmp/bin/mount" <<'EOF'
#!/bin/sh
case $* in *remount,rw*) m=rw ;; *) m=ro ;; esac
echo "/dev/root / ext4 $m 0 0" >"$ETC_ROOT/proc/mounts"
EOF
cat >"$tmp/bin/systemctl" <<'EOF'
#!/bin/sh
[ "$1" = -q ] && shift; [ "$2" = -q ] && set -- "$1" "$3"
case $1 in
is-active) [ -e "$ETC_ROOT/active" ] ;;
is-enabled) [ -e "$ETC_ROOT/enabled" ] ;;
enable) : >"$ETC_ROOT/enabled"; echo "systemctl enable" >>"$ETC_ROOT/calls" ;;
start | restart) : >"$ETC_ROOT/active"; echo "systemctl $1" >>"$ETC_ROOT/calls" ;;
daemon-reload) echo "systemctl daemon-reload" >>"$ETC_ROOT/calls" ;;
esac
EOF
cat >"$tmp/bin/pidof" <<'EOF'
#!/bin/sh
[ -e "$ETC_ROOT/active" ] || [ -e "$ETC_ROOT/hand-run" ]
EOF
cat >"$tmp/work/tailscale" <<'EOF'
#!/bin/sh
case " $* " in
*" status "*) printf '{\n  "Version": "x",\n  "BackendState": "%s",\n' "$(cat "$ETC_ROOT/ts-state" 2>/dev/null || echo NeedsLogin)" ;;
*" up "*)
  for a; do case $a in --auth-key=file:*) cp "${a#--auth-key=file:}" "$ETC_ROOT/seen-key" ;; esac; done
  echo "up $*" >>"$ETC_ROOT/calls"; echo Running >"$ETC_ROOT/ts-state" ;;
esac
EOF
printf '#!/bin/sh\necho tailscaled\n' >"$tmp/work/tailscaled"
chmod +x "$tmp/bin/"* "$tmp/work/"*
: >"$tmp/sshkey"
printf '%s\n' ROBOT_ESN=0 ROBOT_IP=100.64.0.9 ROBOT_LAN_IP=192.0.2.9 PC_LAN_IP=192.0.2.1 ROBOT_SERVER=x "ROBOT_KEY=$tmp/sshkey" >"$tmp/robot.env"
key=tskey-auth-zzTEST0secret
printf '%s\n' "$key" >"$tmp/authkey"
fresh() { local r=$tmp/$1; mkdir -p "$r/proc" "$r/etc"; echo "/dev/root / ext4 ro 0 0" >"$r/proc/mounts"; echo "$r"; }
run() { # <root> <authkey path> [work]
  env ETC_ROOT="$1" PATH="$tmp/bin:$PATH" ROBOT_ENV="$tmp/robot.env" TS_AUTHKEY="$2" TS_WORK="${3:-$tmp/work}" "$here/../apply.sh" 2>&1
}
calls() { grep -vE '^ssh (check|unit)$' "$1/calls" | paste -sd'|'; }
ts_src=100.64.0.7 # the laptop's tailnet IP
held() { # <root> <what>: a tailnet run refused before any push or unit phase
  local out; out=$(FAKE_SRC=$ts_src run "$1" "$tmp/authkey") || die "$2 over the tailnet: $out"
  [[ $out == *"WARN: tailscale: tailscaled upgrade needs LAN (ssh from $ts_src,"*"ROBOT_IP=192.0.2.9/"*"ROBOT_ENV="* && -z $(calls "$1") ]] &&
    ! grep -q '^ssh unit$' "$1/calls" || die "$2 over the tailnet: out=[$out] calls=[$(cat "$1/calls")], want WARN + no push/unit"
}

r=$(fresh nobin)
if out=$(run "$r" "$tmp/authkey" "$tmp/empty"); then die "no binaries passed: $out"; fi
rc=0; run "$r" "$tmp/authkey" "$tmp/empty" >/dev/null || rc=$?
[[ $rc == 2 && ! -e $r/calls ]] || die "no binaries: exit $rc (want 2) or ssh ran"

r=$(fresh nokey)
out=$(run "$r" "$tmp/none") || die "no key: $out"
[[ $out == *"WARN: tailscale: not logged in"* && $(calls "$r") != *up* ]] || die "no key + logged out: out=[$out] calls=[$(calls "$r")]"
for l in 'After=network-pre.target myvector-netguard.service' 'RequiresMountsFor=/data' 'After=mount-data.service' 'Restart=on-failure' 'WantedBy=multi-user.target' \
  "ExecStartPre=/bin/sh -c 'iptables -C OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null || iptables -I OUTPUT -o tailscale0 -j ACCEPT'" \
  "ExecStartPre=/bin/sh -c 'ip6tables -C OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null || ip6tables -I OUTPUT -o tailscale0 -j ACCEPT'" \
  "ExecStopPost=-/bin/sh -c 'while iptables -D OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null; do :; done'" \
  "ExecStopPost=-/bin/sh -c 'while ip6tables -D OUTPUT -o tailscale0 -j ACCEPT 2>/dev/null; do :; done'" \
  'ExecStart=/data/myvector/tailscale/tailscaled --statedir=/data/myvector/tailscale/state/ --socket=/run/tailscale/tailscaled.sock --port=41641 --tun=tailscale0'; do
  grep -qxF -- "$l" "$r/$unit" || die "unit lacks [$l]: $(cat "$r/$unit")"
done
cmp -s "$r/data/myvector/tailscale/tailscaled" "$tmp/work/tailscaled" && [[ -e $r/enabled && -e $r/active ]] || die "no key: binaries, enable or start missing"

r=$(fresh key)
out=$(run "$r" "$tmp/authkey") || die "key: $out"
[[ $(cat "$r/seen-key" 2>/dev/null) == "$key" ]] || die "up did not read the key from its file: out=[$out]"
[[ ! -e $r/run/myvector/tailscale-authkey ]] || die "key copy left in /run"
! grep -qF "$key" "$r/argv" || die "the key value reached ssh argv"
grep -q -- '--hostname=vector --advertise-tags=tag:vector --accept-dns=false --netfilter-mode=off' "$r/calls" || die "up flags: $(grep '^up' "$r/calls")"
want='ssh raw|ssh raw|systemctl daemon-reload|systemctl enable|systemctl start|ssh raw|ssh up|up'
[[ $(calls "$r" | sed 's/|up [^|]*/|up/') == "$want" ]] || die "first run calls [$(calls "$r")], want [$want]"

: >"$r/calls" # nothing changed: a tailnet session passes as before
out=$(FAKE_SRC=$ts_src run "$r" "$tmp/authkey") || die "2nd run: $out"
[[ $out != *changed* && $out != *WARN* && $out == *"logged in (Running)"* && -z $(calls "$r") ]] || die "2nd run wrote: out=[$out] calls=[$(calls "$r")]"

: >"$r/calls"; printf '#!/bin/sh\necho tailscaled v2\n' >"$tmp/work/tailscaled"
held "$r" "new binary"
! cmp -s "$r/data/myvector/tailscale/tailscaled" "$tmp/work/tailscaled" || die "new binary over the tailnet: pushed"
: >"$r/calls"
out=$(run "$r" "$tmp/authkey") || die "new binary: $out"
[[ $(calls "$r") == 'ssh raw|systemctl restart' ]] || die "new binary calls [$(calls "$r")], want one push + restart"

: >"$r/calls"; echo 'Environment=OLD=1' >>"$r/$unit"
held "$r" "unit change"
grep -qx 'Environment=OLD=1' "$r/$unit" || die "unit change over the tailnet: unit rewritten"
: >"$r/calls"
out=$(run "$r" "$tmp/authkey") || die "unit change: $out"
[[ $(calls "$r") == 'systemctl daemon-reload|systemctl enable|systemctl restart' ]] && ! grep -q OLD "$r/$unit" ||
  die "unit change calls [$(calls "$r")], want reload + enable + restart"

r=$(fresh stopped); echo Stopped >"$r/ts-state"
out=$(run "$r" "$tmp/authkey") || die "Stopped: $out"
[[ $out == *"WARN: tailscale: BackendState 'Stopped'"* && ! -e $r/seen-key ]] && ! grep -q '^ssh up$' "$r/calls" || die "a logged-in, non-Running node got the key: out=[$out]"

r=$(fresh hand); : >"$r/hand-run"
if out=$(run "$r" "$tmp/authkey"); then die "hand-run tailscaled passed: $out"; fi
[[ $out == *"outside myvector-tailscaled"* && ! -f $r/$unit ]] || die "hand-run: out=[$out] or unit written"
echo "tailscale apply test OK"

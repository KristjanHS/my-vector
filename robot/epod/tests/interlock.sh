#!/usr/bin/env bash
# Host test of robot/epod/interlock.sh (make check): a fake `openssl s_client` serves a chosen leaf; verify is real openssl.
# Plus: ship.sh and clone_ready (restarts.sh -d, catch.sh -d) run it, failing, before their first robot step.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/epod-interlock-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: epod interlock test: $*" >&2; exit 1; }

"$robot/epod/mkcert.sh" "$tmp/epod" >/dev/null || die "mkcert.sh exited $?"
mkdir "$tmp/bin"
cat >"$tmp/bin/openssl" <<EOF
#!/usr/bin/env bash
if [[ \$1 == s_client ]]; then
  touch "$tmp/probed"
  printf 'CONNECTED(00000003)\n---\nServer certificate\n'; cat "$tmp/served"; printf 'subject=CN = x\n---\n'
  exit 0
fi
exec $(command -v openssl) "\$@"
EOF
chmod +x "$tmp/bin/openssl"
with=$tmp/series-with without=$tmp/series-without renamed=$tmp/series-renamed
printf '0014-deploy-key.patch\n0015-pod-trust.patch\n' >"$with"
printf '0014-deploy-key.patch\n' >"$without"
printf '0014-deploy-key.patch\n0016-pod-trust.patch\n' >"$renamed"
: >"$tmp/key"
printf '%s\n' ROBOT_ESN=0 ROBOT_IP=192.0.2.9 ROBOT_SERVER=x "ROBOT_KEY=$tmp/key" >"$tmp/robot.env"
run() { PATH=$tmp/bin:$PATH ROBOT_ENV=$tmp/robot.env MYVECTOR_EPOD_DIR=${2:-$tmp/epod} "$robot/epod/interlock.sh" "$1" 2>&1; }

# (a) 0015 listed, our leaf served -> ok
cp "$tmp/epod/ep.crt" "$tmp/served"
out=$(run "$with") || die "(a) our leaf served: exit $? [$out]"
[[ -f $tmp/probed ]] || die "(a) passed without probing 127.0.0.1:443"
# (b) 0015 listed, upstream leaf (DDL root) served -> FAIL, do Stage 4.4 first
cp "$robot/../chipper/epod/ep.crt" "$tmp/served"
if out=$(run "$with"); then die "(b) upstream leaf served: exit 0 [$out]"; fi
[[ $out == *'do Stage 4.4 first'* ]] || die "(b) message lacks 'do Stage 4.4 first': [$out]"
# (c) 0015 not listed -> ok, no probe
rm -f "$tmp/probed"
out=$(run "$without") || die "(c) no 0015: exit $? [$out]"
[[ ! -e $tmp/probed ]] || die "(c) probed 127.0.0.1:443 although 0015 is not in series"
# (d) a renumbered *pod-trust.patch still triggers the probe (suffix match)
cp "$robot/../chipper/epod/ep.crt" "$tmp/served"
if out=$(run "$renamed"); then die "(d) 0016-pod-trust.patch + upstream leaf: exit 0 [$out]"; fi
[[ -f $tmp/probed ]] || die "(d) 0016-pod-trust.patch did not probe"
# (e) no ca.crt in EPOD_DIR -> FAIL, do Stage 4.4 first
mkdir "$tmp/empty"
if out=$(run "$with" "$tmp/empty"); then die "(e) no ca.crt: exit 0 [$out]"; fi
[[ $out == *"no $tmp/empty/ca.crt"*'do Stage 4.4 first'* ]] || die "(e) message lacks 'no <dir>/ca.crt … do Stage 4.4 first': [$out]"
# (f) empty s_client output (wire-pod down) -> FAIL
: >"$tmp/served"
if out=$(run "$with"); then die "(f) empty s_client output: exit 0 [$out]"; fi
[[ $out == *'no TLS leaf'* ]] || die "(f) message lacks 'no TLS leaf': [$out]"

# Callers: the interlock line (failing the caller) comes before the first robot step. ncline <file> <ERE>: first non-comment match's line.
ncline() { local m; m=$({ grep -nE "$2" "$1" || :; } | { grep -vE '^[0-9]+:[[:space:]]*#' || :; }); m=${m%%$'\n'*}; echo "${m%%:*}"; }
before() { # <file> <call ERE> <first robot step ERE>
  local c r; c=$(ncline "$1" "$2"); r=$(ncline "$1" "$3")
  [[ -n $c && -n $r ]] || die "${1#"$robot"/}: no line matches [$2] (call: ${c:-none}) or [$3] (robot: ${r:-none})"
  ((c < r)) || die "${1#"$robot"/}: the interlock call (line $c) is not before the first robot step (line $r)"
}
before "$robot/firmware/ship.sh" '"\$head/robot/epod/interlock\.sh" "\$pd/series" \|\| die' \
  'robot/\$sk/apply\.sh|"\$cs"|deploy-v\.sh|"\$pd/settings\.sh"|"\$pd/smoke\.sh"'
before "$robot/firmware/ship.sh" '^ *elif grep -q .pod-trust[^ ]* "\$pd/series"; then die ' 'robot/\$sk/apply\.sh' # no interlock.sh at that sha
before "$robot/crash-review/lib.sh" '"\$fw/\.\./epod/interlock\.sh" "\$fw/series" \|\| \{.*exit [1-9]' 'write_clone_ip'
for f in crash-review/restarts.sh seassert/catch.sh; do before "$robot/$f" '^\(\(deploy\)\) && clone_ready ' 'deploy-v\.sh'; done
echo "epod interlock OK (ours -> pass, upstream leaf / renumbered patch / no ca.crt / no leaf -> FAIL, no pod-trust -> no probe; ship.sh + clone_ready run it first)"

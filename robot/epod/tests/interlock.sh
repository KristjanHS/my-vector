#!/usr/bin/env bash
# Host test of robot/epod/interlock.sh (make check): a fake `openssl s_client` serves a chosen leaf; verify is real openssl.
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
with=$tmp/series-with without=$tmp/series-without
printf '0014-deploy-key.patch\n0015-pod-trust.patch\n' >"$with"
printf '0014-deploy-key.patch\n' >"$without"
run() { PATH=$tmp/bin:$PATH MYVECTOR_EPOD_DIR=$tmp/epod "$robot/epod/interlock.sh" "$1" 2>&1; }

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
echo "epod interlock OK (ours -> pass, upstream leaf -> do Stage 4.4 first, no 0015 -> no probe)"

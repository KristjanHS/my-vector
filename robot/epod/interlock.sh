#!/usr/bin/env bash
# T6 deploy interlock: while <series> lists a *pod-trust.patch (vic-cloud drops the DDL escapepod root), the leaf
# wire-pod serves on 127.0.0.1:443 must verify against $epod_dir/ca.crt, else the robot would lose the server (exit 1).
# Usage: interlock.sh <series>   ship.sh passes the pinned sha's series, clone_ready the tree's; epod_dir: robot/env.sh. Not listed: exit 0, no probe.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
die() { echo "FAIL: pod-trust interlock (T6): $*" >&2; exit 1; }
series=${1:?usage: interlock.sh <series>}
[[ -f $series ]] || die "no series file: $series"
grep -q 'pod-trust\.patch$' "$series" || { echo "pod-trust interlock: no *pod-trust.patch in series, no check"; exit 0; }
# shellcheck source=robot/env.sh
. "$here/../env.sh"
ca=$epod_dir/ca.crt
[[ -f $ca ]] || die "no $ca (pod-trust drops the DDL root the robot trusts today) — do Stage 4.4 first"
leaf=$(openssl s_client -connect 127.0.0.1:443 -servername escapepod.local </dev/null 2>/dev/null |
  awk '/-----BEGIN CERTIFICATE-----/ {p = 1} p {print} /-----END CERTIFICATE-----/ && p {exit}' || true)
[[ -n $leaf ]] || die "no TLS leaf on 127.0.0.1:443 (wire-pod down? make start) — cannot prove it is ours; do Stage 4.4 first"
openssl verify -CAfile "$ca" <(printf '%s\n' "$leaf") >/dev/null 2>&1 ||
  die "the leaf on 127.0.0.1:443 is not signed by $ca: pod-trust would cut the robot off — do Stage 4.4 first"
echo "pod-trust interlock: 127.0.0.1:443 serves a leaf of $ca, pod-trust may deploy"

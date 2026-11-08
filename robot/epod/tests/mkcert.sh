#!/usr/bin/env bash
# Host test of robot/epod/mkcert.sh (make check): chain, SAN, name constraint, DDL root refusal, no overwrite.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
robot=$(cd "$here/../.." && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir 2>/dev/null || echo "$robot/../.git")")
tmp=$(mktemp -d "$main/robot/work/epod-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
die() { echo "FAIL: epod mkcert test: $*" >&2; exit 1; }
o=$tmp/out

"$robot/epod/mkcert.sh" "$o" >/dev/null || die "first run exited $?"
openssl verify -CAfile "$o/ca.crt" "$o/ep.crt" >/dev/null 2>&1 || die "ep.crt does not verify against ca.crt"
san=$(openssl x509 -in "$o/ep.crt" -noout -ext subjectAltName | sed -n '2s/^ *//p')
[[ $san == DNS:escapepod.local ]] || die "leaf SAN = '$san'"
openssl x509 -in "$o/ep.crt" -noout -ext extendedKeyUsage | grep -q 'TLS Web Server Authentication' || die "leaf lacks EKU serverAuth"
openssl x509 -in "$o/ca.crt" -noout -ext basicConstraints | grep -q 'CA:TRUE, pathlen:0' || die "CA lacks pathlen:0"
for c in ca ep; do
  nb=$(date -u -d "$(openssl x509 -in "$o/$c.crt" -noout -startdate | cut -d= -f2)" +%s)
  ((nb <= $(date +%s) - 23 * 3600)) || die "$c.crt notBefore is not backdated a day"
done
[[ $(stat -c %a "$o/ca.key") == 600 && $(stat -c %a "$o/ep.key") == 600 ]] || die "key files not mode 600"

# Name constraint: a leaf for another name, signed by this CA, must not verify.
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$tmp/x.key"
openssl req -new -key "$tmp/x.key" -subj /CN=other.local -out "$tmp/x.csr"
printf 'subjectAltName=DNS:other.local\n' >"$tmp/x.ext"
openssl x509 -req -in "$tmp/x.csr" -CA "$o/ca.crt" -CAkey "$o/ca.key" -set_serial 7 -days 1 -extfile "$tmp/x.ext" -out "$tmp/x.crt" 2>/dev/null
! openssl verify -CAfile "$o/ca.crt" "$tmp/x.crt" >/dev/null 2>&1 || die "CA signs other.local (no name constraint)"

ddl=$main/robot/work/wire-os-victor/cloud/internal/jdocs/escapepod_root_cert.go
if [[ -f $ddl ]]; then
  sed -n '/BEGIN CERTIFICATE/,/END CERTIFICATE/{s/`//g;p}' "$ddl" >"$tmp/ddl.pem"
  openssl verify -CAfile "$tmp/ddl.pem" "$robot/../chipper/epod/ep.crt" >/dev/null 2>&1 || die "DDL root extract does not verify upstream ep.crt"
  ! openssl verify -CAfile "$tmp/ddl.pem" "$o/ep.crt" >/dev/null 2>&1 || die "our leaf verifies against the DDL root"
else echo "epod mkcert: DDL-root check skipped (no wire-os-victor clone)"; fi

sum=$(sha256sum "$o/ca.crt")
rm "$o/ca.key" # the post-copy state: one file left must still refuse
if "$robot/epod/mkcert.sh" "$o" >/dev/null 2>&1; then die "2nd run did not refuse"; fi
[[ $(sha256sum "$o/ca.crt") == "$sum" ]] || die "2nd run changed ca.crt"
echo "epod mkcert host test PASS"

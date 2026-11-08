#!/usr/bin/env bash
# Make the server's own CA + escapepod.local leaf (T1: EC P-256, CA name-constrained to escapepod.local). Refuses to overwrite.
# Usage: robot/epod/mkcert.sh [dir]   default <main checkout>/secrets/epod-new (EPOD_STAGE, gitignored)
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
main=$(dirname "$(git -C "$here" rev-parse --path-format=absolute --git-common-dir)")
out=${1:-$main/secrets/epod-new}
for f in ca.crt ca.key ep.crt ep.key; do
  [[ ! -e $out/$f ]] || { echo "FAIL: $out/$f exists; not overwriting" >&2; exit 1; }
done
umask 077
mkdir -p "$out"
w=$(mktemp -d)
trap 'rm -rf "$w"' EXIT
cat >"$w/cnf" <<EOF
[req]
distinguished_name = dn
[dn]
[ca]
default_ca = own
[own]
database = $w/index.txt
new_certs_dir = $w
serial = $w/serial
default_md = sha256
policy = pol
unique_subject = no
email_in_dn = no
[pol]
commonName = supplied
[ca_ext]
basicConstraints = critical,CA:TRUE,pathlen:0
keyUsage = critical,keyCertSign,cRLSign
nameConstraints = critical,permitted;DNS:escapepod.local
subjectKeyIdentifier = hash
[leaf_ext]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = DNS:escapepod.local
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid
EOF
: >"$w/index.txt"
openssl rand -hex 16 >"$w/serial"
start=$(date -u -d '-1 day' +%Y%m%d%H%M%SZ)
sign() { # <csr> <out> <days> <ext> [ca args…]
  openssl ca -batch -notext -config "$w/cnf" -in "$1" -out "$2" -startdate "$start" -days "$3" -extensions "$4" "${@:5}" 2>"$w/err" ||
    { cat "$w/err" >&2; exit 1; }
}
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$w/ca.key"
openssl req -new -config "$w/cnf" -key "$w/ca.key" -subj "/CN=my-vector escapepod CA" -out "$w/ca.csr"
sign "$w/ca.csr" "$w/ca.crt" 7305 ca_ext -selfsign -keyfile "$w/ca.key"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$w/ep.key"
openssl req -new -config "$w/cnf" -key "$w/ep.key" -subj "/CN=escapepod.local" -out "$w/ep.csr"
sign "$w/ep.csr" "$w/ep.crt" 3653 leaf_ext -cert "$w/ca.crt" -keyfile "$w/ca.key"
cp "$w/ca.key" "$w/ca.crt" "$w/ep.key" "$w/ep.crt" "$out/"
openssl verify -CAfile "$out/ca.crt" "$out/ep.crt"
echo "next: copy $out/ca.key to the password manager, then rm it (nothing on the laptop reads it)"

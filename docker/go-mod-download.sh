#!/usr/bin/env bash
# Download a Go module's dependencies one module at a time, retrying each one.
#
# Why: a single `go mod download` multiplexes hundreds of fetches over one
# HTTP/2 connection to proxy.golang.org. When the proxy resets a stream
# mid-download ("stream error: ... INTERNAL_ERROR; received from peer"), the
# whole step fails and the build starts over. Fetching module by module means a
# reset costs one module, which is retried with backoff. Finished modules stay
# in the module cache, so a retry never refetches them.
#
# Checksums stay enforced: every zip is verified against the go.sum entry it
# already has (a mismatch is a SECURITY ERROR) and `go mod verify` re-checks
# the cache at the end. One difference from a plain `go mod download`: the
# `go mod download mod@ver` form silently ADDS go.sum lines that are missing
# (GOFLAGS=-mod=readonly does not stop it). So go.sum is snapshotted first, and
# if the downloads changed it the script fails: the committed go.sum is
# incomplete, and `go build -mod=readonly` would fail on it anywhere else.
#
# Retries are for network trouble only. Errors that cannot heal on retry
# (checksum mismatch, unknown revision, invalid version, 404/410 from the
# proxy) fail at once.
#
# Usage: go-mod-download.sh [module-dir]          (default: .)
# Env:   GO_MOD_DOWNLOAD_ATTEMPTS  tries per piece (default 5)
#        GO_MOD_DOWNLOAD_BACKOFF   first retry delay in seconds, doubles per retry (default 2)
set -euo pipefail

cd "${1:-.}"
attempts="${GO_MOD_DOWNLOAD_ATTEMPTS:-5}"
backoff="${GO_MOD_DOWNLOAD_BACKOFF:-2}"
permanent='SECURITY ERROR|checksum mismatch|unknown revision|invalid version|404 Not Found|410 Gone'

retry() {
    local what=$1 n=1 delay=$backoff out
    shift
    while :; do
        if out=$("$@" 2>&1); then
            [ -z "$out" ] || printf '%s\n' "$out" >&2
            return 0
        fi
        [ -z "$out" ] || printf '%s\n' "$out" >&2
        if grep -Eq "$permanent" <<<"$out"; then
            echo "go-mod-download: ${what} failed with a permanent error (attempt ${n}/${attempts}), not retrying" >&2
            return 1
        fi
        if [ "$n" -ge "$attempts" ]; then
            echo "go-mod-download: giving up on ${what} after ${n} attempts" >&2
            return 1
        fi
        echo "go-mod-download: ${what} failed (attempt ${n}/${attempts}), retrying in ${delay}s" >&2
        sleep "$delay"
        n=$((n + 1))
        delay=$((delay * 2))
    done
}

# Snapshot go.sum (absent when go.mod has no requires) to detect writes to it.
snap=$(mktemp)
trap 'rm -f "$snap"' EXIT
had_sum=0
if [ -e go.sum ]; then
    cp go.sum "$snap"
    had_sum=1
fi

# The modules go.mod requires (offline: `go mod edit -json` only reads go.mod).
# This is exactly the set a no-argument `go mod download` fetches for go >= 1.17.
# Run it on its own so a failure stops the script instead of yielding 0 modules.
if ! modjson=$(go mod edit -json); then
    echo "go-mod-download: 'go mod edit -json' failed" >&2
    exit 1
fi
parsed=$(awk -F'"' '
    /^\t"Require": \[/ { req = 1; next }
    req && /^\t\]/     { req = 0 }
    req && $2 == "Path"    { path = $4 }
    req && $2 == "Version" { print path "@" $4 }' <<<"$modjson")
mods=()
[ -z "$parsed" ] || mapfile -t mods <<<"$parsed"
if [ "${#mods[@]}" -eq 0 ] && grep -Eq '^[[:space:]]*require([[:space:]]|\(|$)' go.mod; then
    echo "go-mod-download: module list parse failed: go.mod has require entries but none were parsed from 'go mod edit -json'" >&2
    exit 1
fi
echo "go-mod-download: ${#mods[@]} modules"
for mod in "${mods[@]}"; do
    retry "$mod" go mod download "$mod"
done

# Catch-all, retried as a whole: fetches the remaining small go.mod files of the
# module graph and anything the list above missed (e.g. replace targets). Each
# retry only fetches what is still missing from the cache.
retry "go mod download" go mod download

before=/dev/null after=/dev/null
[ "$had_sum" -eq 0 ] || before=$snap
[ ! -e go.sum ] || after=go.sum
if ! cmp -s "$before" "$after"; then
    echo "go-mod-download: ERROR: go.sum is missing entries — run 'go mod tidy' and commit go.sum" >&2
    echo "go-mod-download: entries the download added (diff committed -> patched):" >&2
    diff -u "$before" "$after" >&2 || true
    # Put the committed go.sum back so a rerun fails the same way.
    if [ "$had_sum" -eq 1 ]; then cp "$snap" go.sum; else rm -f go.sum; fi
    exit 1
fi

go mod verify

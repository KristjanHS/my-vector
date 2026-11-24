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
# Checksums stay enforced: every download runs inside the module, so each zip
# is verified against go.sum exactly as a plain `go mod download` would, and
# `go mod verify` re-checks the cache at the end.
#
# Usage: go-mod-download.sh [module-dir]          (default: .)
# Env:   GO_MOD_DOWNLOAD_ATTEMPTS  tries per piece (default 5)
#        GO_MOD_DOWNLOAD_BACKOFF   first retry delay in seconds, doubles per retry (default 2)
set -euo pipefail

cd "${1:-.}"
attempts="${GO_MOD_DOWNLOAD_ATTEMPTS:-5}"
backoff="${GO_MOD_DOWNLOAD_BACKOFF:-2}"

retry() {
    local what=$1 n=1 delay=$backoff
    shift
    until "$@"; do
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

# The modules go.mod requires (offline: `go mod edit -json` only reads go.mod).
# This is exactly the set a no-argument `go mod download` fetches for go >= 1.17.
mapfile -t mods < <(
    go mod edit -json | awk -F'"' '
        /^\t"Require": \[/ { req = 1; next }
        req && /^\t\]/     { req = 0 }
        req && $2 == "Path"    { path = $4 }
        req && $2 == "Version" { print path "@" $4 }'
)
echo "go-mod-download: ${#mods[@]} modules"
for mod in "${mods[@]}"; do
    retry "$mod" go mod download "$mod"
done

# Catch-all, retried as a whole: fetches the remaining small go.mod files of the
# module graph and anything the list above missed (e.g. replace targets). Each
# retry only fetches what is still missing from the cache.
retry "go mod download" go mod download
go mod verify
